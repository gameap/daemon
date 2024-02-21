//go:build windows
// +build windows

package config

const (
	SteamCMDExecutableFile           = "steamcmd.exe"
	DefaultGameServerScriptStart     = ""
	DefaultGameServerScriptStop      = ""
	DefaultGameServerScriptRestart   = ""
	DefaultGameServerScriptStatus    = ""
	DefaultGameServerScriptGetOutput = ""
	DefaultGameServerScriptSendInput = ""
)

const (
	defaultProcessManager = "winsw"
)
