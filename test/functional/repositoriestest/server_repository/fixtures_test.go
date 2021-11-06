package server_repository

var jsonApiGetServerResponse = []byte(`
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
    "cpu_limit": null,
    "ram_limit": null,
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
        "remote_repository": null,
        "local_repository": null
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
        "local_repository": "",
        "default_start_cmd_linux": "./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}",
        "default_start_cmd_windows": "hlds.exe -console -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}",
        "kick_cmd": "kick #{id}",
        "ban_cmd": "amx_ban \"{name}\" {time} \"{reason}\"",
        "chname_cmd": "amx_nick #{id} {name}",
        "srestart_cmd": "restart",
        "chmap_cmd": "changelevel {map}",
        "sendmsg_cmd": "amx_say \"{msg}\"",
        "passwd_cmd": "password {password}"
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
