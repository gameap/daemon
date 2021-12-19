//go:build windows
// +build windows

package commands

const (
	echoTestStringCmd = "powershell Write-Host \"test string\" -nonewline"
	falseCmd          = "powershell exit 1"
)
