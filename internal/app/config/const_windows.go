//go:build windows
// +build windows

package config

const (
	SteamCMDExecutableFile           = "steamcmd.exe"
	DefaultGameServerScriptStart     = "{command}"
	DefaultGameServerScriptStop      = "{command}"
	DefaultGameServerScriptRestart   = "{command}"
	DefaultGameServerScriptStatus    = "{command}"
	DefaultGameServerScriptGetOutput = "{command}"
	DefaultGameServerScriptSendInput = "{command}"
)

func detectDefaultProcessManager() string {
	return "winsw"
}
