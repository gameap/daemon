//go:build darwin
// +build darwin

package files

import (
	"os"
	"syscall"
)

func fileTimeFromFileInfo(fileInfo os.FileInfo) fileTime {
	sys := fileInfo.Sys().(*syscall.Stat_t)

	return fileTime{
		AccessTime:   uint64(sys.Atimespec.Sec),
		CreatingTime: uint64(sys.Ctimespec.Sec),
	}
}
