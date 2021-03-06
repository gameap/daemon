if (CMAKE_COMPILER_IS_GNUCXX)
    set (CMAKE_CXX_FLAGS "--coverage")
endif()

set(SOURCES_CPP
        ${CMAKE_HOME_DIRECTORY}/src/config.cpp
        ${CMAKE_HOME_DIRECTORY}/src/config.cpp
        ${CMAKE_HOME_DIRECTORY}/src/state.cpp
        ${CMAKE_HOME_DIRECTORY}/src/daemon.cpp
        ${CMAKE_HOME_DIRECTORY}/src/daemon_server.cpp
        ${CMAKE_HOME_DIRECTORY}/src/functions/gstring.cpp
        ../mocks/functions/gsystem.cpp
        ${CMAKE_HOME_DIRECTORY}/src/classes/dedicated_server.cpp
        ${CMAKE_HOME_DIRECTORY}/src/classes/game_servers_list.cpp
        ${CMAKE_HOME_DIRECTORY}/src/commands/game_server_installer.cpp
        ${CMAKE_HOME_DIRECTORY}/src/commands/game_server_cmd.cpp
        ${CMAKE_HOME_DIRECTORY}/src/commands/dedicated_server_cmd.cpp
        ${CMAKE_HOME_DIRECTORY}/src/commands/output/cmd_output.cpp
        ${CMAKE_HOME_DIRECTORY}/src/commands/output/ipc_cmd_output.cpp
        ${CMAKE_HOME_DIRECTORY}/src/classes/servers_tasks.cpp
        ${CMAKE_HOME_DIRECTORY}/src/classes/gdaemon_tasks.cpp
        ../mocks/restapi_mock.cpp
        ${CMAKE_HOME_DIRECTORY}/src/functions/auth.cpp
        ${CMAKE_HOME_DIRECTORY}/src/components/server/commands_component.cpp
        ${CMAKE_HOME_DIRECTORY}/src/components/server/files_component.cpp
        ${CMAKE_HOME_DIRECTORY}/src/components/server/status_component.cpp
        )

if(UNIX)
    link_directories(${CMAKE_CURRENT_SOURCE_DIR}/libs)
    set(SOURCES_CPP ${SOURCES_CPP} ${CMAKE_HOME_DIRECTORY}/src/status.cpp)
else()
    set(SOURCES_CPP ${SOURCES_CPP} ../status_vars.cpp)
endif()

add_executable(run_unit_tests
        ${SOURCES_CPP} ${GDAEMON_SOURCES_CPP}
        state_test.cpp
        commands/game_server_installer_test.cpp
        commands/output/cmd_output_test.cpp
        commands/output/ipc_cmd_output_test.cpp
        classes/game_servers_list_test.cpp
        commands/dedicated_server_cmd_test.cpp
        functions/gstring_test.cpp
)

add_compile_definitions(TESTS_ROOT_DIR="${CMAKE_HOME_DIRECTORY}/tests")

target_link_libraries(run_unit_tests
        ${BINN_LIBRARY}
        ${BOOST_LIBS}
        ${RESTCLIENT_LIBRARY}
        ${CURL_LIBRARIES}
        ${JSONCPP_LIBRARY}
        ${OPENSSL_LIBRARIES}
        ${CMAKE_DL_LIBS}
        ${CRYPT_LIBRARY}
        ${ZLIB_LIBRARIES}
        ${CMAKE_THREAD_LIBS_INIT}
        gtest_main
        gmock_main
)

target_compile_definitions(run_unit_tests PRIVATE IPC_OUTPUT_BUFFER_SIZE=100)

add_test(NAME state_test COMMAND run_unit_tests)
add_test(NAME cmd_output COMMAND run_unit_tests)
add_test(NAME ipc_cmd_output COMMAND run_unit_tests)
add_test(NAME game_servers_list COMMAND run_unit_tests)
add_test(NAME dedicated_server_cmd COMMAND run_unit_tests)
add_test(NAME gstring COMMAND run_unit_tests)
