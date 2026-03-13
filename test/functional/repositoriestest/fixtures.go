package repositoriestest

var JSONApiGetServerResponseBody = []byte(`
{
    "id": 1,
    "uuid": "94cdfde4-15a4-40b9-8043-260e6a0b5b67",
    "uuid_short": "94cdfde4",
    "enabled": true,
    "installed": 1,
    "blocked": false,
    "name": "Test",
    "game_id": "cstrike",
    "ds_id": 1,
    "game_mod_id": 4,
    "expires": null,
    "server_ip": "172.24.0.5",
    "server_port": 27015,
    "query_port": 27015,
    "rcon_port": 27015,
    "rcon": "57jPyiVYTO",
    "dir": "servers/94cdfde4-15a4-40b9-8043-260e6a0b5b67",
    "su_user": "gameap",
    "cpu_limit": 2000,
    "ram_limit": 2147483648,
    "net_limit": null,
    "start_command": "./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}",
    "stop_command": null,
    "force_stop_command": null,
    "restart_command": null,
    "process_active": true,
    "last_process_check": "2021-11-05 19:57:11",
    "vars": {
		"default_map": "de_dust2"
	},
    "created_at": "2021-11-05T15:01:27.000000Z",
    "updated_at": "2021-11-05T19:57:11.000000Z",
    "deleted_at": null,
    "game": {
        "code": "cstrike",
        "start_code": "cstrike",
        "name": "Counter-Strike 1.6",
        "engine": "GoldSource",
        "engine_version": "1",
        "steam_app_id": 90,
        "steam_app_set_config": null,
        "remote_repository": "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
        "local_repository": "/srv/gameap/repository/hlcs_base.tar.xz",
        "metadata": {
            "custom_key": "custom_value"
        }
    },
    "game_mod": {
        "id": 4,
        "game_code": "cstrike",
        "name": "Classic (Standart)",
        "fast_rcon": [
            {
                "info": "Status",
                "command": "status"
            },
            {
                "info": "Stats",
                "command": "stats"
            },
            {
                "info": "Last disconnect players",
                "command": "amx_last"
            },
            {
                "info": "Admins on servers",
                "command": "amx_who"
            }
        ],
        "vars": [
            {
                "var": "default_map",
                "default": "de_dust2",
                "info": "Default Map",
                "admin_var": false
            },
            {
                "var": "fps",
                "default": 500,
                "info": "Server FPS (tickrate)",
                "admin_var": true
            },
            {
                "var": "maxplayers",
                "default": 32,
                "info": "Maximum players on server",
                "admin_var": false
            }
        ],
        "remote_repository": "http://files.gameap.ru/cstrike-1.6/amxx.tar.xz",
        "local_repository": "/srv/gameap/repository/cstrike-1.6/amxx.tar.xz",
        "default_start_cmd_linux": "./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}",
        "default_start_cmd_windows": "hlds.exe -console -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}",
        "kick_cmd": "kick #{id}",
        "ban_cmd": "amx_ban \"{name}\" {time} \"{reason}\"",
        "chname_cmd": "amx_nick #{id} {name}",
        "srestart_cmd": "restart",
        "chmap_cmd": "changelevel {map}",
        "sendmsg_cmd": "amx_say \"{msg}\"",
        "passwd_cmd": "password {password}",
        "metadata": {
            "mod_key": "mod_value"
        }
    },
    "settings": [
        {
            "id": 1,
            "name": "autostart_current",
            "server_id": 1,
            "value": "1"
        }
    ]
}`)

var JSONApiGetTokenResponseBody = []byte(`
{
    "token": "dYCw9ADVnS03leY9dLlckgaxiG59uKF3KMCcpmXpJUKYmlQXuAhvHtCYbL6hG3Ce",
	"timestamp": 0
}
`)

var JSONApiGetServersTasks = []byte(`
[
    {
        "id": 1,
        "command": "restart",
        "server_id": 1,
        "repeat": 0,
        "repeat_period": 600,
        "counter": 0,
        "execute_date": "2021-11-14 00:00:00",
        "payload": null,
        "created_at": "2021-11-13T11:41:32.000000Z",
        "updated_at": "2021-11-13T12:44:41.000000Z"
    }
]
`)

var JSONApiGetServerFactorioResponseBody = []byte(`
{
    "id": 2,
    "uuid": "9c3dea74-b4d6-4e2f-9f4e-6c97b6e3f9a2",
    "uuid_short": "9c3dea74",
    "enabled": true,
    "installed": 1,
    "blocked": false,
    "name": "Factorio Test Server",
    "game_id": "factorio",
    "ds_id": 1,
    "game_mod_id": 10,
    "expires": null,
    "server_ip": "192.168.1.100",
    "server_port": 27023,
    "query_port": 27023,
    "rcon_port": 27023,
    "rcon": "factoriorcon",
    "dir": "servers/9c3dea74-b4d6-4e2f-9f4e-6c97b6e3f9a2",
    "su_user": "gameap",
    "cpu_limit": 4000,
    "ram_limit": 4294967296,
    "net_limit": null,
    "start_command": "./factorio --start-server {SAVE_NAME}.zip --server-settings server-settings.json",
    "stop_command": null,
    "force_stop_command": null,
    "restart_command": null,
    "process_active": false,
    "last_process_check": "2024-01-15 10:30:00",
    "vars": null,
    "created_at": "2024-01-15T08:00:00.000000Z",
    "updated_at": "2024-01-15T10:30:00.000000Z",
    "deleted_at": null,
    "game": {
        "code": "factorio",
        "start_code": "factorio",
        "name": "Factorio",
        "engine": "Factorio",
        "engine_version": "1",
        "steam_app_id": 427520,
        "steam_app_set_config": null,
        "remote_repository": "http://files.gameap.ru/factorio/factorio_headless.tar.xz",
        "local_repository": "/srv/gameap/repository/factorio_headless.tar.xz",
        "metadata": null
    },
    "game_mod": {
        "id": 10,
        "game_code": "factorio",
        "name": "Vanilla",
        "fast_rcon": null,
        "vars": [
            {
                "var": "FACTORIO_VERSION",
                "default": "latest",
                "info": "Factorio Version",
                "admin_var": true
            },
            {
                "var": "MAX_SLOTS",
                "default": 10,
                "info": "Maximum player slots",
                "admin_var": false
            },
            {
                "var": "SAVE_NAME",
                "default": "world",
                "info": "Save file name",
                "admin_var": false
            },
            {
                "var": "SERVER_DESC",
                "default": "A Factorio Server",
                "info": "Server Description",
                "admin_var": false
            }
        ],
        "remote_repository": "http://files.gameap.ru/factorio/vanilla.tar.xz",
        "local_repository": "/srv/gameap/repository/factorio/vanilla.tar.xz",
        "default_start_cmd_linux": "./factorio --start-server {SAVE_NAME}.zip --server-settings server-settings.json",
        "default_start_cmd_windows": "factorio.exe --start-server {SAVE_NAME}.zip --server-settings server-settings.json",
        "kick_cmd": "/kick {name}",
        "ban_cmd": "/ban {name} {reason}",
        "chname_cmd": null,
        "srestart_cmd": null,
        "chmap_cmd": null,
        "sendmsg_cmd": null,
        "passwd_cmd": null,
        "metadata": null
    },
    "settings": [
        {
            "id": 10,
            "name": "update_before_start",
            "server_id": 2,
            "value": "false"
        },
        {
            "id": 11,
            "name": "SERVER_DESC",
            "server_id": 2,
            "value": "Description"
        },
        {
            "id": 12,
            "name": "SERVER_USERNAME",
            "server_id": 2,
            "value": "unnamed"
        },
        {
            "id": 13,
            "name": "FACTORIO_VERSION",
            "server_id": 2,
            "value": "1.1.100"
        },
        {
            "id": 14,
            "name": "MAX_SLOTS",
            "server_id": 2,
            "value": "20"
        },
        {
            "id": 15,
            "name": "SAVE_NAME",
            "server_id": 2,
            "value": "gamesave"
        }
    ]
}`)
