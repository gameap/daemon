package fsutil

import (
	"os"

	"github.com/pkg/errors"
)

// Rename moves src to dst. Two paths confined to the same directory are
// renamed inside it. Otherwise each side keeps to its own root, so neither can
// be led out of its confinement by a symlink standing in the other.
func Rename(src, dst *Confined) error {
	if src.SameRoot(dst) {
		return src.Root.Rename(src.Rel, dst.Rel)
	}

	return renameAcross(src.Root, src.Rel, dst.Root, dst.Rel)
}

// moveByCopy moves an entry between two roots where the platform has no rename
// relative to two directory handles. It refuses an existing destination: a
// rename would replace it atomically, and a copy can only merge into it.
func moveByCopy(srcRoot *os.Root, src string, dstRoot *os.Root, dst string) error {
	if _, err := dstRoot.Lstat(dst); err == nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: os.ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := CopyTree(srcRoot, src, dstRoot, dst, CopyOptions{}); err != nil {
		_ = dstRoot.RemoveAll(dst)

		return err
	}

	if err := srcRoot.RemoveAll(src); err != nil {
		return errors.Wrap(err, "failed to remove the source after copying it")
	}

	return nil
}
