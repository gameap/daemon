//go:build linux
// +build linux

package config

const SteamCMDExecutableFile = "steamcmd.sh"

const DefaultGameServerScriptStart = "{node_work_path}/runner.sh " +
	"start -d {dir} -n {uuid} -u {user} -c \"{command}\""

const DefaultGameServerScriptStop = "{node_work_path}/runner.sh " +
	"stop -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptRestart = "{node_work_path}/runner.sh " +
	"restart -d {dir} -n {uuid} -u {user} -c \"{command}\""

const DefaultGameServerScriptStatus = "{node_work_path}/runner.sh " +
	"status -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptGetOutput = "{node_work_path}/runner.sh " +
	"get_console -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptSendInput = "{node_work_path}/runner.sh " +
	"send_command -d {dir} -n {uuid} -u {user} -c \"{command}\""
