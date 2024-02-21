//go:build linux || darwin
// +build linux darwin

package config

const (
	SteamCMDExecutableFile           = "steamcmd.sh"
	DefaultGameServerScriptStart     = "{command}"
	DefaultGameServerScriptStop      = "{command}"
	DefaultGameServerScriptRestart   = "{command}"
	DefaultGameServerScriptStatus    = "{command}"
	DefaultGameServerScriptGetOutput = "{command}"
	DefaultGameServerScriptSendInput = "{command}"
)

const (
	defaultProcessManager = "tmux"
)
