//go:build !windows && !plan9
// +build !windows,!plan9

package serverscommand

const (
	FailScript        = "./fail.sh"
	CommandScript     = "./command.sh"
	CommandFailScript = "./command_fail.sh"

	// CommandResultFile is the file command.sh appends its arguments to,
	// relative to the directory the script runs in.
	CommandResultFile = "command_result.txt"
)
