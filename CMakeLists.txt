cmake_minimum_required(VERSION 3.20)

# Vectorworks 2026 のプラグイン（cli）。設計は docs/plugin/、ビルドの形は
# docs/plugin/build-and-release.md。SDK に依らない共通部（src/core）とその単体テスト・ファジングと、
# SDK をリンクするプラグイン本体を持つ。
#
# プラグイン（SDK が要る。macOS か Windows で、Release でビルドする）:
#   cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DVW_SDK_DIR=<SDKLib を含むフォルダ>  # mac
#   cmake -S . -B build -A x64 -DVW_SDK_DIR=<SDKLib を含むフォルダ>                     # Windows
#   cmake --build build --config Release --target VwCli
#
# 単体テスト（SDK は要らない）:
#   cmake -S . -B build -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON
#   cmake --build build
#   ctest --test-dir build --output-on-failure

option(VW_BUILD_PLUGIN "Build the plug-in (needs VW_SDK_DIR; macOS or Windows)" ON)
option(VW_BUILD_TESTS "Build the SDK-free unit tests" OFF)
option(VW_BUILD_FUZZERS "Build the libFuzzer targets (clang only)" OFF)
option(VW_ENABLE_SANITIZERS "Build with AddressSanitizer and UndefinedBehaviorSanitizer" OFF)
option(VW_ENABLE_COVERAGE "Build for coverage (clang source-based coverage, or MSVC with /PROFILE)" OFF)
option(VW_ENABLE_PCH "Precompile the SDK headers for the plug-in" ON)

# mac のプラグインは arm64 だけ（SDK リファレンスの plugin/ が実機で読み込まれている形）。
# project() より前に決める。テストだけのビルドは手元のアーキテクチャのまま。
if(VW_BUILD_PLUGIN AND CMAKE_HOST_APPLE)
	if(NOT DEFINED CMAKE_OSX_ARCHITECTURES)
		set(CMAKE_OSX_ARCHITECTURES "arm64" CACHE STRING "Target architectures")
	endif()
	if(NOT DEFINED CMAKE_OSX_DEPLOYMENT_TARGET)
		set(CMAKE_OSX_DEPLOYMENT_TARGET "14.0" CACHE STRING "Minimum macOS version")
	endif()
endif()

project(VwCli LANGUAGES CXX)
# mac の SDK のヘッダは Objective-C++ として読む（CoreFoundation などを #import するため）。
if(VW_BUILD_PLUGIN AND APPLE)
	enable_language(OBJCXX)
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# 警告と言語の厳しさは SDK に依らないターゲット（共通部・テスト・ファジング）にだけ付ける。
# SDK のヘッダは /permissive- を通る保証が無いので、プラグインには付けない（下記）。
function(vw_strict target)
	if(MSVC)
		# ソースは UTF-8（日本語の文字列を含む）。
		target_compile_options(${target} PRIVATE /utf-8 /W4 /permissive- /EHsc)
	else()
		target_compile_options(${target} PRIVATE -Wall -Wextra -Wpedantic)
	endif()
endfunction()

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

# カバレッジ（test.yml が集めて Codecov へ送る）。clang（Apple の clang を含む）は source-based
# coverage で、llvm-cov で集める。MSVC は Visual Studio の Microsoft.CodeCoverage.Console が
# 実行ファイルを PDB から計装して集めるので、コンパイルは変えず、計装に要る /PROFILE を付けて
# リンクするだけ（/PROFILE は /INCREMENTAL と両立しないので、Debug の既定の /INCREMENTAL を
# 明示して止め、LNK4075 を出さない）。ASan の付いた実行ファイルの計装は確かめていないので、
# 併用は止める。clang-cl はリンカーが link.exe・lld-link で clang の指定を受け付けないので弾く。
if(VW_ENABLE_COVERAGE)
	if(MSVC AND NOT CMAKE_CXX_COMPILER_ID MATCHES "Clang")
		if(VW_ENABLE_SANITIZERS)
			message(FATAL_ERROR "VW_ENABLE_COVERAGE with MSVC cannot be combined with VW_ENABLE_SANITIZERS")
		endif()
		add_link_options(/PROFILE /INCREMENTAL:NO)
	elseif(CMAKE_CXX_COMPILER_ID MATCHES "Clang" AND NOT MSVC)
		add_compile_options(-fprofile-instr-generate -fcoverage-mapping)
		add_link_options(-fprofile-instr-generate)
	else()
		message(FATAL_ERROR "VW_ENABLE_COVERAGE needs MSVC or clang with the GNU-style driver (not clang-cl)")
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
add_library(VwCliCore STATIC src/core/Bridge.cpp src/core/Json.cpp src/core/Ping.cpp
						   src/core/Serve.cpp)
if(WIN32)
	target_sources(VwCliCore PRIVATE src/core/FilesWindows.cpp)
	target_link_libraries(VwCliCore PUBLIC shell32 ole32)
else()
	target_sources(VwCliCore PRIVATE src/core/FilesPosix.cpp)
endif()
target_include_directories(VwCliCore PUBLIC src)
vw_strict(VwCliCore)

# --- プラグイン（SDK に依る） -----------------------------------------------------------------
# 形は SDK リファレンスの plugin/CMakeLists.txt（実機で読み込まれている形）に合わせる。殻と本体の
# 分割・同梱スクリプト・版のファイルは持たない（docs/plugin/build-and-release.md「CMake」）。
if(VW_BUILD_PLUGIN)
	if(NOT APPLE AND NOT WIN32)
		message(FATAL_ERROR "The plug-in builds only on macOS or Windows (the SDK has no other "
							"platform). Configure with -DVW_BUILD_PLUGIN=OFF to build the tests.")
	endif()
	if(NOT DEFINED VW_SDK_DIR)
		message(FATAL_ERROR "VW_SDK_DIR is not set: pass the folder that contains SDKLib, or "
							"-DVW_BUILD_PLUGIN=OFF to build the tests only")
	endif()
	set(VW_SDK_INCLUDE "${VW_SDK_DIR}/SDKLib/Include")
	if(APPLE)
		set(VW_SDK_LIB "${VW_SDK_DIR}/SDKLib/LibMac/Release")
		set(VW_SDK_LIBS "${VW_SDK_LIB}/libVWSDK.a")
		set(VW_BUILDVWR "${VW_SDK_DIR}/SDKLib/ToolsMac/BuildVWR/BuildVWR")
	else()
		# SDK のライブラリは x64 だけ（-A x64 で構成する）。
		set(VW_SDK_LIB "${VW_SDK_DIR}/SDKLib/LibWin/Release")
		set(VW_SDK_LIBS "${VW_SDK_LIB}/VWSDK.lib" "${VW_SDK_LIB}/VWMM.lib")
		set(VW_BUILDVWR "${VW_SDK_DIR}/SDKLib/ToolsWin/BuildVWR/buildvwr.exe")
	endif()
	foreach(required IN ITEMS "${VW_SDK_INCLUDE}/VectorworksSDK.h" ${VW_SDK_LIBS} "${VW_BUILDVWR}")
		if(NOT EXISTS "${required}")
			message(FATAL_ERROR "${required} is missing (VW_SDK_DIR=${VW_SDK_DIR})")
		endif()
	endforeach()

	# 刻印（docs/plugin/build-and-release.md「リリースの形」）。ping の結果にだけ載る。
	set(VW_BUILD_VERSION "local" CACHE STRING "Short sha of the build (ping version)")
	set(VW_BUILD_BRANCH "local" CACHE STRING "Branch of the build (ping branch)")

	set(VW_PLUGIN_SOURCES src/Clock.cpp src/ModuleMain.cpp src/tools/Ping.cpp
						  src/tools/ToolTable.cpp)

	# add_vw_plugin はプラグイン 1 つ（name はファイル名・フォルダ名・.vwr の名前）。
	function(add_vw_plugin target name bundleId)
		add_library(${target} MODULE ${VW_PLUGIN_SOURCES})
		set_target_properties(${target} PROPERTIES OUTPUT_NAME "${name}")
		target_link_libraries(${target} PRIVATE VwCliCore)
		target_include_directories(${target} PRIVATE src)
		# SDK のヘッダの警告は出さない。
		target_include_directories(
			${target} SYSTEM PRIVATE "${VW_SDK_INCLUDE}" "${VW_SDK_INCLUDE}/Kernel"
			"${VW_SDK_INCLUDE}/Interfaces" "${VW_SDK_INCLUDE}/VWMM")
		# RELEASE_BLD は SDK の Release のライブラリに合わせる。
		target_compile_definitions(
			${target} PRIVATE RELEASE_BLD=1 "VW_BUILD_VERSION=\"${VW_BUILD_VERSION}\""
			"VW_BUILD_BRANCH=\"${VW_BUILD_BRANCH}\"")
		if(VW_ENABLE_PCH)
			target_precompile_headers(${target} PRIVATE src/PluginPrefix.h)
		endif()
		set(vwr "${CMAKE_CURRENT_SOURCE_DIR}/resources/${name}.vwr")
		if(APPLE)
			set_source_files_properties(${VW_PLUGIN_SOURCES} TARGET_DIRECTORY ${target}
										PROPERTIES LANGUAGE OBJCXX)
			target_include_directories(${target} SYSTEM PRIVATE "${VW_SDK_INCLUDE}/OnlyMac")
			set(VW_PLUGIN_NAME "${name}")
			set(VW_BUNDLE_ID "${bundleId}")
			set(plist "${CMAKE_CURRENT_BINARY_DIR}/${name}-Info.plist")
			configure_file(src/Module-Info.plist.in "${plist}" @ONLY)
			set_target_properties(
				${target} PROPERTIES OBJCXX_STANDARD 20 OBJCXX_STANDARD_REQUIRED ON BUNDLE TRUE
				BUNDLE_EXTENSION "vwlibrary" MACOSX_BUNDLE_INFO_PLIST "${plist}")
			target_compile_options(${target} PRIVATE -stdlib=libc++ -Wno-deprecated-declarations)
			target_link_libraries(${target} PRIVATE ${VW_SDK_LIBS} "-F${VW_SDK_LIB}"
								  "-framework VWMM" "-framework Carbon" "-framework Cocoa")
			set(resources "$<TARGET_BUNDLE_CONTENT_DIR:${target}>/Resources")
			add_custom_command(
				TARGET ${target} POST_BUILD
				COMMAND "${CMAKE_COMMAND}" -E make_directory "${resources}"
				COMMAND "${CMAKE_COMMAND}" -E env "VWR_INPUT=${vwr}" "VWR_OUTPUT=${resources}/${name}.vwr"
						"VWR_MODE=Release" "${VW_BUILDVWR}"
				VERBATIM)
		else()
			target_include_directories(${target} SYSTEM PRIVATE "${VW_SDK_INCLUDE}/OnlyWin")
			# /MD は SDK のライブラリに合わせる。
			set_target_properties(${target} PROPERTIES PREFIX "" SUFFIX ".vlb"
									  MSVC_RUNTIME_LIBRARY "MultiThreadedDLL")
			# _WINDOWS が SDK のヘッダを GS_WIN の構成にする（MSVC は自分では定義しない）。
			target_compile_definitions(
				${target} PRIVATE _WINDOWS WIN32 _USRDLL _WINDLL _WIN_EXTERNAL_ UNICODE _UNICODE
				NDEBUG _CRT_SECURE_NO_WARNINGS)
			# /utf-8: ソースは UTF-8（日本語の文字列を含む）。/bigobj: SDK のヘッダが既定の
			# セクションの上限を超える。
			target_compile_options(${target} PRIVATE /utf-8 /EHsc /bigobj /wd4996 /MP)
			target_link_libraries(${target} PRIVATE ${VW_SDK_LIBS} USP10.lib Rpcrt4.lib)
			add_custom_command(
				TARGET ${target} POST_BUILD
				COMMAND "${CMAKE_COMMAND}" -E env "VWR_INPUT=${vwr}"
						"VWR_OUTPUT=$<TARGET_FILE_DIR:${target}>/${name}.vwr" "VWR_MODE=Release"
						"VWR_7ZCMD=${VW_SDK_DIR}/SDKLib/ToolsWin/BuildVWR/7z" "${VW_BUILDVWR}"
				VERBATIM)
		endif()
	endfunction()

	add_vw_plugin(VwCli "cli" io.github.min-nano.cli)
endif()

# --- 単体テスト ---------------------------------------------------------------------------
if(VW_BUILD_TESTS)
	enable_testing()
	add_executable(VwCliTests tests/BridgeTests.cpp tests/FixtureTests.cpp tests/JsonTests.cpp
							  tests/PingTests.cpp tests/ServeTests.cpp tests/TestMain.cpp
							  tests/TestSupport.cpp)
	target_link_libraries(VwCliTests PRIVATE VwCliCore)
	vw_strict(VwCliTests)
	# 通信規則の見本（C++ と Go のテストが同じものを読む）。
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
	vw_strict(JsonFuzzer)
	target_link_options(JsonFuzzer PRIVATE -fsanitize=fuzzer)
	if(VW_BUILD_TESTS)
		# 種（要求の見本）を 1 回ずつ通すだけ。決まった時間だけ回すのは CI（test.yml）。
		add_test(NAME JsonFuzzerSeeds
				 COMMAND JsonFuzzer -runs=0 ${CMAKE_CURRENT_SOURCE_DIR}/protocol/fixtures/requests)
	endif()
endif()

# --- カバレッジ -----------------------------------------------------------------------------
if(VW_ENABLE_COVERAGE AND VW_BUILD_TESTS AND NOT MSVC)
	# clang だけ。MSVC の結果は Microsoft.CodeCoverage.Console が 1 回の実行ごとに書き出す。
	# 前の実行の結果が混ざらないよう、テストの前に消す（同じビルドでテストを繰り返すと、変えた
	# 行が前の結果で通ったと数えられる）。ctest -R で絞っても、準備のテストは一緒に走る。
	set(coverageDir ${CMAKE_BINARY_DIR}/coverage)
	add_test(NAME CoverageClean COMMAND ${CMAKE_COMMAND} -E rm -rf ${coverageDir})
	set_tests_properties(CoverageClean PROPERTIES FIXTURES_SETUP coverage)
	# 計測付きのテストはすべて結果を同じ場所に書く（%p はプロセスごとに分ける）。ENVIRONMENT は
	# 置き換えではなく足して、ほかの指定を消さない。
	foreach(test IN ITEMS VwCliTests JsonFuzzerSeeds)
		if(TEST ${test})
			set_property(TEST ${test} APPEND
						 PROPERTY ENVIRONMENT "LLVM_PROFILE_FILE=${coverageDir}/%p.profraw")
			set_property(TEST ${test} APPEND PROPERTY FIXTURES_REQUIRED coverage)
		endif()
	endforeach()
endif()
