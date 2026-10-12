cmake_minimum_required(VERSION 3.20)

#
# cli - a Vectorworks 2026 plug-in that keeps a bridge resident inside Vectorworks
# so that the bundled command `vw2026` (cli/) and any program that follows the
# protocol (docs/protocol.md) can drive Vectorworks from outside.
#
# This file builds the SDK-free half today:
#
#   * VwCliCore   - src/core/ (Json, Bridge, Serve). It never includes the
#                   Vectorworks SDK, so it builds and is unit-tested on any
#                   toolchain (docs/plugin/architecture.md).
#   * the tests   - tests/ (VW_BUILD_TESTS), including the protocol fixtures shared
#                   with the Go tests (protocol/fixtures/).
#   * the fuzzers - tests/fuzz/ (VW_BUILD_FUZZERS, Clang's libFuzzer only).
#
# The plug-in itself (VW_BUILD_PLUGIN, the SDK, .vwlibrary / .vlb) is added in the
# next step of docs/plugin/plan.md.
#
#   cmake -S . -B build -DVW_BUILD_TESTS=ON -DVW_ENABLE_SANITIZERS=ON
#   cmake --build build --parallel
#   ctest --test-dir build --output-on-failure
#

project(VwCli LANGUAGES CXX)

# --- Build options -----------------------------------------------------------
#   * VW_BUILD_TESTS       (OFF) -> the SDK-free unit tests.
#   * VW_ENABLE_SANITIZERS (OFF) -> AddressSanitizer + UndefinedBehaviorSanitizer
#                                   for VwCliCore and the tests (GCC / Clang).
#   * VW_ENABLE_COVERAGE   (OFF) -> gcov instrumentation for VwCliCore and the tests.
#   * VW_BUILD_FUZZERS     (OFF) -> libFuzzer entry points (Clang only), built with
#                                   ASan + UBSan.
option(VW_BUILD_TESTS "Build the unit tests" OFF)
option(VW_ENABLE_SANITIZERS "Build/run the tests under AddressSanitizer + UBSan" OFF)
option(VW_ENABLE_COVERAGE "Instrument the tests for code coverage (gcov)" OFF)
option(VW_BUILD_FUZZERS "Build the libFuzzer entry points (Clang only)" OFF)

if((VW_ENABLE_SANITIZERS OR VW_ENABLE_COVERAGE) AND NOT CMAKE_CXX_COMPILER_ID MATCHES "GNU|Clang")
    message(FATAL_ERROR "VW_ENABLE_SANITIZERS / VW_ENABLE_COVERAGE require GCC or Clang.")
endif()
if(VW_BUILD_FUZZERS AND NOT CMAKE_CXX_COMPILER_ID MATCHES "Clang")
    message(FATAL_ERROR "VW_BUILD_FUZZERS requires Clang (libFuzzer).")
endif()

# Flags shared by every SDK-free target, so that the library is checked / measured
# exactly like the tests that link it.
set(VW_SANITIZER_FLAGS -g -fno-omit-frame-pointer -fsanitize=address,undefined
                       -fno-sanitize-recover=all)
set(VW_COVERAGE_FLAGS -O0 -g --coverage)

# vw_sdk_free_flags(<target>): warnings as errors plus the optional instrumentation.
function(vw_sdk_free_flags target)
    target_compile_features(${target} PUBLIC cxx_std_20)
    if(MSVC)
        # /utf-8: the sources are UTF-8 (Japanese comments and messages).
        target_compile_options(${target} PRIVATE /W4 /WX /utf-8)
    else()
        target_compile_options(${target} PRIVATE -Wall -Wextra -Werror)
    endif()
    if(VW_ENABLE_SANITIZERS)
        target_compile_options(${target} PRIVATE ${VW_SANITIZER_FLAGS})
        target_link_options(${target} PRIVATE -fsanitize=address,undefined)
    endif()
    if(VW_ENABLE_COVERAGE)
        target_compile_options(${target} PRIVATE ${VW_COVERAGE_FLAGS})
        target_link_options(${target} PRIVATE --coverage)
    endif()
endfunction()

# --- SDK-free core -------------------------------------------------------------
# The plug-in and the tests link the same library; it includes only src/core/
# headers and the OS (never the SDK).
add_library(VwCliCore STATIC src/core/Bridge.cpp src/core/Json.cpp src/core/Serve.cpp)
target_include_directories(VwCliCore PUBLIC "${CMAKE_CURRENT_SOURCE_DIR}/src")
vw_sdk_free_flags(VwCliCore)
if(WIN32)
    # SHGetKnownFolderPath (shell32) and CoTaskMemFree (ole32) for the spool location.
    target_link_libraries(VwCliCore PUBLIC shell32 ole32)
endif()

if(VW_BUILD_TESTS)
    enable_testing()
    add_subdirectory(tests)
endif()

if(VW_BUILD_FUZZERS)
    add_subdirectory(tests/fuzz)
endif()
