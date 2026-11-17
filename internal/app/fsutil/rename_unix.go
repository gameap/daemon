//go:build linux || darwin

package fsutil

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

// renameAcross renames an entry of one root into another. The directory
// holding each name is opened through its own root, which is where symlinks
// are followed and checked; renameat then acts on the two directory entries
// themselves and follows nothing.
func renameAcross(srcRoot *os.Root, src string, dstRoot *os.Root, dst string) error {
	srcDir, err := srcRoot.Open(path.Dir(src))
	if err != nil {
		return err
	}
	defer srcDir.Close()

	dstDir, err := dstRoot.Open(path.Dir(dst))
	if err != nil {
		return err
	}
	defer dstDir.Close()

	err = unix.Renameat(int(srcDir.Fd()), path.Base(src), int(dstDir.Fd()), path.Base(dst))
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: src, New: dst, Err: err}
	}

	return nil
}
