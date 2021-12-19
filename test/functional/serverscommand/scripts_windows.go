//go:build windows
// +build windows

package serverscommand

const (
	FailScript        = "cmd /c fail.bat"
	CommandScript     = "cmd /c command.bat"
	CommandFailScript = "cmd /c command_fail.bat"
)
