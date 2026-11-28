# 3.24: CMAKE_COMPILE_WARNING_AS_ERROR（CI とローカル環境でのテストが警告をエラーにする）が
# 有効になる最初の版。
# 3.20〜3.23 では警告なく無視され、警告があってもビルドが成功する。
cmake_minimum_required(VERSION 3.24)

# Vectorworks 2026 のプラグイン（cli）。設計は docs/plugin/、ビルドの構成は
# docs/plugin/build-and-release.md。SDK に依存しない共通部（src/core）とその単体テスト・
# ファジングと、SDK をリンクするプラグイン本体を有する。
#
# プラグイン（SDK が必要。macOS か Windows で、Release でビルドする）:
#   cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DVW_SDK_DIR=<SDKLib を含むフォルダ>  # mac
#   cmake -S . -B build -A x64 -DVW_SDK_DIR=<SDKLib を含むフォルダ>                     # Windows
#   cmake --build build --config Release --target VwCli
# ローカル環境（mac）では CMakePresets.json のプリセット vw2026 を使用する（RelWithDebInfo。
# docs/plugin/build-and-release.md「ローカル環境でのビルド」）。
#
# 単体テスト（SDK は不要。CLAUDE.md「C++」のローカル環境でのテストと同じ指定。CI の cpp.yml は
# カバレッジも計測する）:
#   cmake -S . -B build -DVW_BUILD_PLUGIN=OFF -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON \
#     -DCMAKE_COMPILE_WARNING_AS_ERROR=ON -DCMAKE_BUILD_TYPE=Debug
#   cmake --build build
#   ctest --test-dir build --output-on-failure

option(VW_BUILD_PLUGIN "Build the plug-in (needs VW_SDK_DIR; macOS or Windows)" ON)
option(VW_BUILD_TESTS "Build the SDK-free unit tests" OFF)
option(VW_BUILD_FUZZERS "Build the libFuzzer targets (clang only)" OFF)
option(VW_ENABLE_SANITIZERS "Build with AddressSanitizer and UndefinedBehaviorSanitizer" OFF)
option(VW_ENABLE_COVERAGE "Build for coverage (clang source-based coverage, or MSVC with /PROFILE)" OFF)
option(VW_ENABLE_PCH "Precompile the SDK headers for the plug-in" ON)

# mac のプラグインは arm64 のみ（SDK リファレンスの plugin/ が実機で読み込まれている構成）。
# project() より前に決定する。テストのみのビルドはローカル環境のアーキテクチャのまま。
if(VW_BUILD_PLUGIN AND CMAKE_HOST_APPLE)
	if(NOT DEFINED CMAKE_OSX_ARCHITECTURES)
		set(CMAKE_OSX_ARCHITECTURES "arm64" CACHE STRING "Target architectures")
	endif()
	if(NOT DEFINED CMAKE_OSX_DEPLOYMENT_TARGET)
		set(CMAKE_OSX_DEPLOYMENT_TARGET "14.0" CACHE STRING "Minimum macOS version")
	endif()
endif()

project(VwCli LANGUAGES CXX)
# mac の SDK のヘッダは Objective-C++ として読み込む（CoreFoundation などを #import するため）。
if(VW_BUILD_PLUGIN AND APPLE)
	enable_language(OBJCXX)
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# 警告と言語の厳格さは SDK に依存しないターゲット（共通部・テスト・ファジング）にのみ付与する。
# SDK のヘッダは /permissive- を通過する保証が無いため、プラグインには付与しない（下記）。
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

# カバレッジ（cpp.yml が収集して Codecov へ送信する）。clang（Apple の clang を含む）は source-based
# coverage で、llvm-cov で収集する。MSVC は Visual Studio の Microsoft.CodeCoverage.Console が
# 実行ファイルを PDB から計装して収集するため、コンパイルは変更せず、計装に必要な /PROFILE を付けて
# リンクするのみ（/PROFILE は /INCREMENTAL と両立しないため、Debug の既定の /INCREMENTAL を明示的に
# 無効にし、LNK4075 を出さない）。ASan の付いた実行ファイルの計装は検証していないため、併用は
# 禁止する。clang-cl はリンカーが link.exe・lld-link で clang の指定を受け付けないため拒否する。
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

# --- 共通部（SDK に依存しない。プラグインとテストがリンクする） -------------------------------
add_library(VwCliCore STATIC src/core/Args.cpp src/core/Bridge.cpp src/core/DocumentTools.cpp
						   src/core/Files.cpp src/core/Json.cpp src/core/Ping.cpp src/core/Quit.cpp
						   src/core/ReadTools.cpp src/core/SaveTarget.cpp src/core/Serve.cpp)
if(WIN32)
	# WindowsPath は \\?\ を付ける処理で、スプールと保存先の検証が共に使用する。
	target_sources(VwCliCore PRIVATE src/core/FilesWindows.cpp src/core/SaveTargetWindows.cpp
						   src/core/WindowsPath.cpp)
else()
	target_sources(VwCliCore PRIVATE src/core/FilesPosix.cpp src/core/SaveTargetPosix.cpp)
	# dladdr（モジュールの場所。glibc 2.34 より前は libdl にある。mac は空）。
	target_link_libraries(VwCliCore PUBLIC ${CMAKE_DL_LIBS})
endif()
# save の to を開いている図面と比較するラッパー（NFC と大文字小文字を区別しない比較）は、出荷する
# OS の呼び出しでのみ作成する（Linux の代替は配置しない）。作成する場合のみフラグを定義し、テスト
# （tests/PathCompareTests.cpp）はこのフラグで内容を有効にする。OS で分岐する条件はここのみ。
if(APPLE)
	target_sources(VwCliCore PRIVATE src/core/PathCompareMac.cpp)
	target_link_libraries(VwCliCore PUBLIC "-framework CoreFoundation")
	target_compile_definitions(VwCliCore PUBLIC VWCLI_HAS_PATH_COMPARE=1)
elseif(WIN32)
	target_sources(VwCliCore PRIVATE src/core/PathCompareWindows.cpp)
	target_link_libraries(VwCliCore PUBLIC Normaliz)
	target_compile_definitions(VwCliCore PUBLIC VWCLI_HAS_PATH_COMPARE=1)
endif()
target_include_directories(VwCliCore PUBLIC src)
vw_strict(VwCliCore)

# --- プラグイン（SDK に依存する） -------------------------------------------------------------
# 構成は SDK リファレンスの plugin/CMakeLists.txt（実機で読み込まれている構成）に統一する。殻と
# 本体の分割・同梱スクリプト・版のファイルは有しない（docs/plugin/build-and-release.md「CMake」）。
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
		# SDK のライブラリは x64 のみ（-A x64 で構成する）。
		set(VW_SDK_LIB "${VW_SDK_DIR}/SDKLib/LibWin/Release")
		set(VW_SDK_LIBS "${VW_SDK_LIB}/VWSDK.lib" "${VW_SDK_LIB}/VWMM.lib")
		set(VW_BUILDVWR "${VW_SDK_DIR}/SDKLib/ToolsWin/BuildVWR/buildvwr.exe")
	endif()
	foreach(required IN ITEMS "${VW_SDK_INCLUDE}/VectorworksSDK.h" ${VW_SDK_LIBS} "${VW_BUILDVWR}")
		if(NOT EXISTS "${required}")
			message(FATAL_ERROR "${required} is missing (VW_SDK_DIR=${VW_SDK_DIR})")
		endif()
	endforeach()

	# 刻印（docs/plugin/build-and-release.md「リリースの構成」）。ping の結果にのみ含まれる。
	# -DVW_BUILD_VERSION / -DVW_BUILD_BRANCH で渡した場合のみその値を使用し、渡さなければ local。
	# キャッシュに残さない（残ると、後で渡さずに構成し直しても古い sha のまま ping に含まれる）。
	# C の文字列に記述するため \ と " をエスケープする（git はブランチ名に " を許容する）。生成した
	# ヘッダに記述し、コンパイルの指定の引用符の扱いに依存しない。
	foreach(stamp IN ITEMS VERSION BRANCH)
		if(DEFINED VW_BUILD_${stamp})
			set(value "${VW_BUILD_${stamp}}")
		else()
			set(value "local")
		endif()
		unset(VW_BUILD_${stamp} CACHE)
		string(REPLACE "\\" "\\\\" value "${value}")
		string(REPLACE "\"" "\\\"" value "${value}")
		set(VW_BUILD_${stamp}_LITERAL "${value}")
	endforeach()
	file(CONFIGURE OUTPUT "${CMAKE_CURRENT_BINARY_DIR}/generated/BuildStamp.h"
		 CONTENT "#pragma once\n// CMake が書く（CMakeLists.txt）。編集しない。\n#define VW_BUILD_VERSION \"@VW_BUILD_VERSION_LITERAL@\"\n#define VW_BUILD_BRANCH \"@VW_BUILD_BRANCH_LITERAL@\"\n"
		 @ONLY)

	set(VW_PLUGIN_SOURCES src/Clock.cpp src/ModuleMain.cpp src/tools/Documents.cpp
						  src/tools/Ping.cpp src/tools/ReadTools.cpp src/tools/ToolTable.cpp)

	# add_vw_plugin はプラグイン 1 つ（name はファイル名・フォルダ名・.vwr の名前）。
	function(add_vw_plugin target name bundleId)
		add_library(${target} MODULE ${VW_PLUGIN_SOURCES})
		set_target_properties(${target} PROPERTIES OUTPUT_NAME "${name}")
		target_link_libraries(${target} PRIVATE VwCliCore)
		# src は VwCliCore の PUBLIC の指定で追加される。
		target_include_directories(${target} PRIVATE "${CMAKE_CURRENT_BINARY_DIR}/generated")
		# SDK のヘッダの警告は出力しない。
		target_include_directories(
			${target} SYSTEM PRIVATE "${VW_SDK_INCLUDE}" "${VW_SDK_INCLUDE}/Kernel"
			"${VW_SDK_INCLUDE}/Interfaces" "${VW_SDK_INCLUDE}/VWMM")
		# RELEASE_BLD は SDK の Release のライブラリに統一する。
		target_compile_definitions(${target} PRIVATE RELEASE_BLD=1)
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
			# /MD は SDK のライブラリに統一する。
			set_target_properties(${target} PROPERTIES PREFIX "" SUFFIX ".vlb"
									  MSVC_RUNTIME_LIBRARY "MultiThreadedDLL")
			# _WINDOWS が SDK のヘッダを GS_WIN の構成にする（MSVC は自身では定義しない）。
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
	# PathCompareTests.cpp はいずれの OS でも列挙する（内容は VWCLI_HAS_PATH_COMPARE で囲み、
	# Linux では空。mac と Windows でフラグが無ければコンパイルで停止する）。
	add_executable(VwCliTests tests/ArgsTests.cpp tests/BridgeTests.cpp tests/DocumentToolsTests.cpp
							  tests/FixtureTests.cpp tests/JsonTests.cpp tests/NumberTests.cpp
							  tests/PathCompareTests.cpp tests/PingTests.cpp tests/QuitTests.cpp
							  tests/ReadToolsTests.cpp tests/SaveTargetTests.cpp
							  tests/ServeTests.cpp tests/TestMain.cpp tests/TestSupport.cpp)
	target_link_libraries(VwCliTests PRIVATE VwCliCore)
	vw_strict(VwCliTests)
	# 通信規則のフィクスチャ（C++ と Go のテストが同じものを読み込む）。
	target_compile_definitions(VwCliTests
							   PRIVATE VWCLI_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/protocol/fixtures")
	# ツールの結果の規則のフィクスチャ（通信規則のフィクスチャとは別。C++ と Go のテストが同じものを
	# 読み込む）。
	target_compile_definitions(VwCliTests
							   PRIVATE VWCLI_TOOL_FIXTURES_DIR="${CMAKE_CURRENT_SOURCE_DIR}/tests/fixtures")
	add_test(NAME VwCliTests COMMAND VwCliTests)
	# 開いて待機しないことのテスト（FIFO）は、退行すると失敗ではなく停止として現れる。CI のジョブの
	# 上限まで待機しないよう、上限を設ける（通常は数秒で終了する）。
	set_tests_properties(VwCliTests PROPERTIES TIMEOUT 120)
endif()

# --- ファジング -----------------------------------------------------------------------------
if(VW_BUILD_FUZZERS)
	add_executable(JsonFuzzer tests/JsonFuzzer.cpp)
	target_link_libraries(JsonFuzzer PRIVATE VwCliCore)
	vw_strict(JsonFuzzer)
	target_link_options(JsonFuzzer PRIVATE -fsanitize=fuzzer)
	if(VW_BUILD_TESTS)
		# シード（要求のフィクスチャ）を 1 回ずつ通過させるのみ。定められた時間のみ実行するのは CI
		# （cpp.yml）。
		add_test(NAME JsonFuzzerSeeds
				 COMMAND JsonFuzzer -runs=0 ${CMAKE_CURRENT_SOURCE_DIR}/protocol/fixtures/requests)
	endif()
endif()

# --- カバレッジ -----------------------------------------------------------------------------
if(VW_ENABLE_COVERAGE AND VW_BUILD_TESTS AND NOT MSVC)
	# clang のみ。MSVC の結果は Microsoft.CodeCoverage.Console が 1 回の実行ごとに書き出す。
	# 前回の実行の結果が混在しないよう、テストの前に削除する（同じビルドでテストを繰り返すと、
	# 変更した行が前回の結果で通過したと計上される）。ctest -R で絞り込んでも、準備のテストは併せて
	# 実行される。
	set(coverageDir ${CMAKE_BINARY_DIR}/coverage)
	add_test(NAME CoverageClean COMMAND ${CMAKE_COMMAND} -E rm -rf ${coverageDir})
	set_tests_properties(CoverageClean PROPERTIES FIXTURES_SETUP coverage)
	# 計測付きのテストは全て結果を同じ場所に書き込む（%p はプロセスごとに分ける）。ENVIRONMENT は
	# 置換ではなく追加し、他の指定を削除しない。
	foreach(test IN ITEMS VwCliTests JsonFuzzerSeeds)
		if(TEST ${test})
			set_property(TEST ${test} APPEND
						 PROPERTY ENVIRONMENT "LLVM_PROFILE_FILE=${coverageDir}/%p.profraw")
			set_property(TEST ${test} APPEND PROPERTY FIXTURES_REQUIRED coverage)
		endif()
	endforeach()
endif()
