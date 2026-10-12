# libFuzzer entry points (Clang only). Enable with -DVW_BUILD_FUZZERS=ON; they are
# built with ASan + UBSan so that a memory error or undefined behaviour stops the
# run. CI (.github/workflows/test.yml) runs them for a fixed time with the seeds.

# vw_add_fuzzer(<name> <sources...>): one fuzzer linking an instrumented copy of the
# core sources (VwCliCore itself is built without -fsanitize=fuzzer).
function(vw_add_fuzzer name)
	add_executable(${name} ${ARGN} ${CMAKE_SOURCE_DIR}/src/core/Bridge.cpp
												 ${CMAKE_SOURCE_DIR}/src/core/Json.cpp)
	target_include_directories(${name} PRIVATE "${CMAKE_SOURCE_DIR}/src")
	target_compile_features(${name} PRIVATE cxx_std_20)
	target_compile_options(
		${name}
		PRIVATE -g
						-O1
						-fno-omit-frame-pointer
						-fsanitize=fuzzer,address,undefined
						-fno-sanitize-recover=all
						-Wall
						-Wextra
						-Werror)
	target_link_options(${name} PRIVATE -fsanitize=fuzzer,address,undefined)
endfunction()

vw_add_fuzzer(JsonFuzz JsonFuzz.cpp)
