//go:build windows
// +build windows

package config

const SteamCMDExecutableFile = "steamcmd.exe"

const DefaultGameServerScriptStart = "{node_work_path}/daemon/gameap-starter.exe " +
	"start -d {dir} -c \"{command}\""

const DefaultGameServerScriptStop = "{node_work_path}/daemon/gameap-starter.exe " +
	"stop -d {dir}"

const DefaultGameServerScriptRestart = "{node_work_path}/daemon/gameap-starter.exe " +
	"restart -d {dir} -c \"{command}\""

const DefaultGameServerScriptStatus = "{node_work_path}/daemon/gameap-starter.exe " +
	"status -d {dir}"

const DefaultGameServerScriptGetOutput = ""

const DefaultGameServerScriptSendInput = ""
