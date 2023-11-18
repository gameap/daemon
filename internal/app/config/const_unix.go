//go:build linux || darwin
// +build linux darwin

package config

const SteamCMDExecutableFile = "steamcmd.sh"

const RunnerExecutablePathFile = "{node_work_path}/runner.sh"

const DefaultGameServerScriptStart = RunnerExecutablePathFile + " " +
	"start -d {dir} -n {uuid} -u {user} -c \"{command}\""

const DefaultGameServerScriptStop = RunnerExecutablePathFile + " " +
	"stop -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptRestart = RunnerExecutablePathFile + " " +
	"restart -d {dir} -n {uuid} -u {user} -c \"{command}\""

const DefaultGameServerScriptStatus = RunnerExecutablePathFile + " " +
	"status -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptGetOutput = RunnerExecutablePathFile + " " +
	"get_console -d {dir} -n {uuid} -u {user}"

const DefaultGameServerScriptSendInput = RunnerExecutablePathFile + " " +
	"send_command -d {dir} -n {uuid} -u {user} -c \"{command}\""
