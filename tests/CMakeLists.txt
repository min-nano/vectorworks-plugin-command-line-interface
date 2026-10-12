# Unit tests for the SDK-free core (src/core/). None of them links the Vectorworks
# SDK, so they build and run on any toolchain, including a plain Linux runner.
# Enable with -DVW_BUILD_TESTS=ON (see the top-level CMakeLists.txt).

# vw_add_test(<name> <sources...>): one test executable linking VwCliCore. The
# protocol fixtures (protocol/fixtures/) are passed in as VW_PROTOCOL_FIXTURES_DIR.
function(vw_add_test name)
	add_executable(${name} ${ARGN})
	target_include_directories(${name} PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}")
	target_link_libraries(${name} PRIVATE VwCliCore)
	vw_sdk_free_flags(${name})
	target_compile_definitions(
		${name} PRIVATE "VW_PROTOCOL_FIXTURES_DIR=\"${CMAKE_SOURCE_DIR}/protocol/fixtures\"")
	add_test(NAME ${name} COMMAND ${name})
endfunction()

# core/Json: round trips, the strict grammar, the nesting limit and UTF-8.
vw_add_test(CoreJsonTests CoreJsonTests.cpp)

# core/Bridge: the spool (lock, sweep, poll, take, wait files, replies) and the
# request / response shapes.
vw_add_test(CoreBridgeTests CoreBridgeTests.cpp)

# core/Serve: one round of serving against a fake tool table (sessions, refusals,
# quit, exceptions).
vw_add_test(CoreServeTests CoreServeTests.cpp)

# protocol/fixtures/: the constants and the serving examples shared with the Go
# fake plug-in (cli/internal/fakeplugin).
vw_add_test(ProtocolFixturesTests ProtocolFixturesTests.cpp)
