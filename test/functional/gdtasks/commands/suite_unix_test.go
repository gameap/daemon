//go:build !windows && !plan9
// +build !windows,!plan9

package commands

const (
	MakeFileWithContentsServerScript = "./server/make_file_with_contents.sh"
	MakeFileWithContentsScript       = "./make_file_with_contents.sh"
	FailScript                       = "./fail.sh"
	StartCommandScript               = "./append_to_file.sh start"
	StopCommandScript                = "./append_to_file.sh stop"
	SleepAndCheckScript              = "./sleep_and_check.sh"
)
