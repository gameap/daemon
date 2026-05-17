package fsutil

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
)

// SymlinkAction selects how a symlink encountered during a copy is handled.
type SymlinkAction int

const (
	// SymlinkShallow recreates the symlink with the same target without
	// dereferencing or recursing into it. This is the zero value and matches
	// the default behaviour of the recursive-copy library this package replaced.
	SymlinkShallow SymlinkAction = iota
	// SymlinkDeep follows the symlink and copies the target's contents.
	SymlinkDeep
	// SymlinkSkip ignores symlinks entirely.
	SymlinkSkip
)

// CopyOptions tunes a copy operation.
type CopyOptions struct {
	Symlink SymlinkAction
	// AddPermission is OR'd into the mode of every copied regular file.
	AddPermission os.FileMode
}

// CopyInRoot copies src to dst where both are paths relative to the same root.
func CopyInRoot(root *os.Root, src, dst string, opts CopyOptions) error {
	return CopyTree(root, src, root, dst, opts)
}

// CopyTree copies srcRoot:src into dstRoot:dst. Directories are merged into an
// existing destination and regular files overwrite an existing one, matching
// the default behaviour of the recursive-copy library this package replaced.
func CopyTree(srcRoot *os.Root, src string, dstRoot *os.Root, dst string, opts CopyOptions) error {
	info, err := srcRoot.Lstat(src)
	if err != nil {
		return errors.Wrap(err, "failed to stat copy source")
	}

	return copyEntry(srcRoot, src, dstRoot, dst, info, opts)
}

// Copy copies absolute src to absolute dst. It is a convenience for trusted,
// operator-controlled callers (game-server installation, test fixtures): only
// the top-level destination container is created with os.MkdirAll, while every
// recursive read and write — where symlink-escape risk lives — stays confined
// to an os.Root.
func Copy(src, dst string, opts CopyOptions) error {
	info, err := os.Lstat(src)
	if err != nil {
		return errors.Wrap(err, "failed to stat copy source")
	}

	if info.IsDir() {
		srcRoot, err := os.OpenRoot(src)
		if err != nil {
			return errors.Wrap(err, "failed to open source root")
		}
		defer srcRoot.Close()

		if err := os.MkdirAll(dst, 0o755); err != nil {
			return errors.Wrap(err, "failed to create destination directory")
		}

		dstRoot, err := os.OpenRoot(dst)
		if err != nil {
			return errors.Wrap(err, "failed to open destination root")
		}
		defer dstRoot.Close()

		return CopyTree(srcRoot, ".", dstRoot, ".", opts)
	}

	srcRoot, err := os.OpenRoot(filepath.Dir(src))
	if err != nil {
		return errors.Wrap(err, "failed to open source root")
	}
	defer srcRoot.Close()

	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return errors.Wrap(err, "failed to create destination directory")
	}

	dstRoot, err := os.OpenRoot(dstDir)
	if err != nil {
		return errors.Wrap(err, "failed to open destination root")
	}
	defer dstRoot.Close()

	return CopyTree(srcRoot, filepath.Base(src), dstRoot, filepath.Base(dst), opts)
}

func copyEntry(
	srcRoot *os.Root, src string, dstRoot *os.Root, dst string, info fs.FileInfo, opts CopyOptions,
) error {
	mode := info.Mode()

	switch {
	case mode&os.ModeSymlink != 0:
		return copySymlink(srcRoot, src, dstRoot, dst, opts)
	case mode.IsDir():
		return copyDir(srcRoot, src, dstRoot, dst, info, opts)
	case mode.IsRegular():
		return copyFile(srcRoot, src, dstRoot, dst, info, opts)
	default:
		// Sockets, fifos and device nodes are skipped: a running game server
		// keeps unix sockets in its work directory and copying them is both
		// impossible and unwanted.
		return nil
	}
}

func copyDir(
	srcRoot *os.Root, src string, dstRoot *os.Root, dst string, info fs.FileInfo, opts CopyOptions,
) error {
	if err := ensureDir(dstRoot, dst, dirPerm(info)); err != nil {
		return err
	}

	entries, err := fs.ReadDir(srcRoot.FS(), src)
	if err != nil {
		return errors.Wrap(err, "failed to read source directory")
	}

	for _, entry := range entries {
		childSrc := path.Join(src, entry.Name())
		childDst := path.Join(dst, entry.Name())

		childInfo, err := srcRoot.Lstat(childSrc)
		if err != nil {
			return errors.Wrap(err, "failed to stat directory entry")
		}

		if err := copyEntry(srcRoot, childSrc, dstRoot, childDst, childInfo, opts); err != nil {
			return err
		}
	}

	return nil
}

func copyFile(
	srcRoot *os.Root, src string, dstRoot *os.Root, dst string, info fs.FileInfo, opts CopyOptions,
) error {
	if err := ensureParent(dstRoot, dst); err != nil {
		return err
	}

	srcFile, err := srcRoot.Open(src)
	if err != nil {
		return errors.Wrap(err, "failed to open source file")
	}
	defer srcFile.Close()

	perm := (info.Mode() | opts.AddPermission).Perm()

	dstFile, err := dstRoot.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return errors.Wrap(err, "failed to create destination file")
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return errors.Wrap(err, "failed to copy file contents")
	}

	if err := dstFile.Close(); err != nil {
		return errors.Wrap(err, "failed to close destination file")
	}

	// Restore the exact source mode: OpenFile is subject to umask, which would
	// otherwise strip the executable bit from game-server scripts.
	if err := dstRoot.Chmod(dst, perm); err != nil {
		return errors.Wrap(err, "failed to set destination file mode")
	}

	return nil
}

func copySymlink(
	srcRoot *os.Root, src string, dstRoot *os.Root, dst string, opts CopyOptions,
) error {
	switch opts.Symlink {
	case SymlinkSkip:
		return nil
	case SymlinkDeep:
		target, err := srcRoot.Stat(src)
		if err != nil {
			return errors.Wrap(err, "failed to stat symlink target")
		}

		switch {
		case target.IsDir():
			return copyDir(srcRoot, src, dstRoot, dst, target, opts)
		case target.Mode().IsRegular():
			return copyFile(srcRoot, src, dstRoot, dst, target, opts)
		default:
			return nil
		}
	default: // SymlinkShallow
		linkTarget, err := srcRoot.Readlink(src)
		if err != nil {
			return errors.Wrap(err, "failed to read symlink")
		}

		if err := ensureParent(dstRoot, dst); err != nil {
			return err
		}

		_ = dstRoot.Remove(dst)

		if err := dstRoot.Symlink(linkTarget, dst); err != nil {
			return errors.Wrap(err, "failed to create symlink")
		}

		return nil
	}
}

func ensureParent(root *os.Root, name string) error {
	return ensureDir(root, path.Dir(name), 0o755)
}

func ensureDir(root *os.Root, name string, perm os.FileMode) error {
	if name == "." || name == "" || name == "/" {
		return nil
	}

	if err := root.MkdirAll(name, perm); err != nil {
		return errors.Wrap(err, "failed to create directory")
	}

	return nil
}

func dirPerm(info fs.FileInfo) os.FileMode {
	if perm := info.Mode().Perm(); perm != 0 {
		return perm
	}

	return 0o755
}
