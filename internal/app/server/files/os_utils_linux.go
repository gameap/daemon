//go:build linux
// +build linux

package files

import (
	"os"
	"syscall"
)

func fileTimeFromFileInfo(fileInfo os.FileInfo) fileTime {
	sys := fileInfo.Sys().(*syscall.Stat_t)

	return fileTime{
		AccessTime:   uint64(sys.Atim.Sec),
		CreatingTime: uint64(sys.Ctim.Sec),
	}
}
