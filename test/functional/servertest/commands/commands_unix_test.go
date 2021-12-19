//go:build !windows && !plan9
// +build !windows,!plan9

package commands

const (
	echoTestStringCmd = "echo -n \"test string\""
	falseCmd          = "false"
)
