//go:build windows
// +build windows

package files

import (
	"os"
	"syscall"
	"time"
)

func fileTimeFromFileInfo(fileInfo os.FileInfo) fileTime {
	sys := fileInfo.Sys().(*syscall.Win32FileAttributeData)
	return fileTime{
		AccessTime:   uint64(sys.LastAccessTime.Nanoseconds() / int64(time.Second)),
		CreatingTime: uint64(sys.CreationTime.Nanoseconds() / int64(time.Second)),
	}
}
