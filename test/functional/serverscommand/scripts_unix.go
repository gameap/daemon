//go:build !windows && !plan9
// +build !windows,!plan9

package serverscommand

const (
	FailScript        = "./fail.sh"
	CommandScript     = "./command.sh"
	CommandFailScript = "./command_fail.sh"
)
