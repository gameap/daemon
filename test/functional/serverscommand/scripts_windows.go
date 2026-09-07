//go:build windows
// +build windows

package serverscommand

const (
	FailScript        = "cmd /c fail.bat"
	CommandScript     = "cmd /c command.bat"
	CommandFailScript = "cmd /c command_fail.bat"

	// CommandResultFile is the file command.bat appends its arguments to,
	// relative to the directory the script runs in.
	CommandResultFile = "file.txt"
)
