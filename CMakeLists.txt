cmake_minimum_required(VERSION 3.20)

# Vectorworks 2026 のプラグイン（cli）。設計は docs/plugin/、ビルドの形は
# docs/plugin/build-and-release.md。いまは SDK に依らない共通部（src/core）とその単体テスト・
# ファジングだけを持つ（プラグイン本体は実装の順序の段 2 で足す。docs/plugin/plan.md）。
#
#   cmake -S . -B build -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON
#   cmake --build build
#   ctest --test-dir build --output-on-failure
project(VwCli LANGUAGES CXX)

option(VW_BUILD_TESTS "Build the SDK-free unit tests" OFF)
option(VW_BUILD_FUZZERS "Build the libFuzzer targets (clang only)" OFF)
option(VW_ENABLE_SANITIZERS "Build with AddressSanitizer and UndefinedBehaviorSanitizer" OFF)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

if(MSVC)
	# ソースは UTF-8（日本語の文字列を含む）。
	add_compile_options(/utf-8 /W4 /permissive- /EHsc)
else()
	add_compile_options(-Wall -Wextra -Wpedantic)
endif()

if(VW_ENABLE_SANITIZERS)
	if(MSVC)
		# MSVC には UndefinedBehaviorSanitizer が無い。
		add_compile_options(/fsanitize=address)
	else()
		add_compile_options(-fsanitize=address,undefined -fno-sanitize-recover=all
							-fno-omit-frame-pointer)
		add_link_options(-fsanitize=address,undefined)
	endif()
endif()

if(VW_BUILD_FUZZERS)
	if(NOT CMAKE_CXX_COMPILER_ID MATCHES "Clang" OR CMAKE_CXX_COMPILER_ID STREQUAL "AppleClang")
		message(FATAL_ERROR "VW_BUILD_FUZZERS needs clang with libFuzzer (Apple clang has none)")
	endif()
	# 共通部も計装し、ファジングが分岐を辿れるようにする。
	add_compile_options(-fsanitize=fuzzer-no-link)
endif()

# --- 共通部（SDK に依らない。プラグインとテストがリンクする） ---------------------------------
add_library(VwCliCore STATIC src/core/Bridge.cpp src/core/Json.cpp src/core/Serve.cpp)
if(WIN32)
	target_sources(VwCliCore PRIVATE src/core/FilesWindows.cpp)
	target_link_libraries(VwCliCore PUBLIC shell32 ole32)
else()
	target_sources(VwCliCore PRIVATE src/core/FilesPosix.cpp)
endif()
target_include_directories(VwCliCore PUBLIC src)

# --- 単体テスト ---------------------------------------------------------------------------
if(VW_BUILD_TESTS)
	enable_testing()
	add_executable(VwCliTests tests/BridgeTests.cpp tests/FixtureTests.cpp tests/JsonTests.cpp
							  tests/ServeTests.cpp tests/TestMain.cpp tests/TestSupport.cpp)
	target_link_libraries(VwCliTests PRIVATE VwCliCore)
	# 作法の見本（C++ と Go のテストが同じものを読む）。
	target_compile_definitions(VwCliTests
							   PRIVATE VWCLI_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/protocol/fixtures")
	add_test(NAME VwCliTests COMMAND VwCliTests)
	# 開いて待たないことのテスト（FIFO）は、退行すると失敗ではなく止まって現れる。CI のジョブの
	# 上限まで待たないよう、上限を付ける（ふだんは数秒で終わる）。
	set_tests_properties(VwCliTests PROPERTIES TIMEOUT 120)
endif()

# --- ファジング -----------------------------------------------------------------------------
if(VW_BUILD_FUZZERS)
	add_executable(JsonFuzzer tests/JsonFuzzer.cpp)
	target_link_libraries(JsonFuzzer PRIVATE VwCliCore)
	target_link_options(JsonFuzzer PRIVATE -fsanitize=fuzzer)
	if(VW_BUILD_TESTS)
		# 種（要求の見本）を 1 回ずつ通すだけ。決まった時間だけ回すのは CI（test.yml）。
		add_test(NAME JsonFuzzerSeeds
				 COMMAND JsonFuzzer -runs=0 ${CMAKE_CURRENT_SOURCE_DIR}/protocol/fixtures/requests)
	endif()
endif()
