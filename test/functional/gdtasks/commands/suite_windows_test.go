//go:build windows
// +build windows

package commands

const (
	MakeFileWithContentsServerScript = "powershell ./server/make_file_with_contents.ps1"
	MakeFileWithContentsScript       = "powershell ./make_file_with_contents.ps1"
	FailScript                       = "cmd /c fail.bat"
	StartCommandScript               = "cmd /c append_to_file.bat start"
	StopCommandScript                = "cmd /c append_to_file.bat stop"
	SleepAndCheckScript              = "powershell ./sleep_and_check.ps1"
)
