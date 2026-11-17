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

// ErrCopyIntoItself is returned when the destination of a directory copy is
// the source directory or lies inside it. Such a copy never ends on its own:
// it keeps finding, and copying, what it has just written.
var ErrCopyIntoItself = errors.New("cannot copy a directory into itself")

// ErrCopySameFile is returned when a file would be copied onto itself: its
// destination is the same file under this or another name. Emptying the
// destination to write it would empty the source.
var ErrCopySameFile = errors.New("source and destination are the same file")

// CopyInRoot copies src to dst where both are paths relative to the same root.
func CopyInRoot(root *os.Root, src, dst string, opts CopyOptions) error {
	return CopyTree(root, src, root, dst, opts)
}

// CopyTree copies srcRoot:src into dstRoot:dst. Directories are merged into an
// existing destination and regular files overwrite an existing one, matching
// the default behaviour of the recursive-copy library this package replaced.
//
// A directory is not copied into itself, and no file onto itself: the copy
// fails with ErrCopyIntoItself or ErrCopySameFile.
func CopyTree(srcRoot *os.Root, src string, dstRoot *os.Root, dst string, opts CopyOptions) error {
	info, err := srcRoot.Lstat(src)
	if err != nil {
		return errors.Wrap(err, "failed to stat copy source")
	}

	if err := refuseOverlap(info, dstRoot, dst); err != nil {
		return errors.WithMessagef(err, "failed to copy %q to %q", src, dst)
	}

	_, dstErr := dstRoot.Lstat(dst)

	c := &copier{srcRoot: srcRoot, dstRoot: dstRoot, opts: opts}

	err = c.entry(src, dst, info)
	// A refused copy leaves nothing behind, as long as all of it is its own.
	if errors.Is(err, ErrCopyIntoItself) && errors.Is(dstErr, os.ErrNotExist) {
		_ = dstRoot.RemoveAll(dst)
	}

	return err
}

// refuseOverlap rejects, before anything is written, the copy of a directory
// to itself or to one of the directories its destination is created in. The
// two are compared as directories, not by name: they are named through
// different roots, and one directory can be spelled in more than one way.
//
// A destination whose path leads into the source from outside of dstRoot, or
// through a link, is not seen from here; copier.dir stops that copy as soon as
// it reaches the destination. A file is compared with its destination where it
// is written, see copier.file.
func refuseOverlap(src fs.FileInfo, dstRoot *os.Root, dst string) error {
	if !src.IsDir() {
		return nil
	}

	for dir := dst; ; dir = path.Dir(dir) {
		if info, err := dstRoot.Stat(dir); err == nil && os.SameFile(src, info) {
			return ErrCopyIntoItself
		}

		if dir == "." || dir == "/" {
			return nil
		}
	}
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

// copier carries one CopyTree call through the source tree.
type copier struct {
	srcRoot *os.Root
	dstRoot *os.Root
	opts    CopyOptions
	// dstTop is the directory the copy writes into, known once it is created.
	dstTop fs.FileInfo
}

func (c *copier) entry(src, dst string, info fs.FileInfo) error {
	mode := info.Mode()

	switch {
	case mode&os.ModeSymlink != 0:
		return c.symlink(src, dst)
	case mode.IsDir():
		return c.dir(src, dst, info)
	case mode.IsRegular():
		return c.file(src, dst, info)
	default:
		// Sockets, fifos and device nodes are skipped: a running game server
		// keeps unix sockets in its work directory and copying them is both
		// impossible and unwanted.
		return nil
	}
}

func (c *copier) dir(src, dst string, info fs.FileInfo) error {
	// The destination turned up inside the source. Descending into it would
	// copy the copy, one level deeper each time, until the disk or the stack
	// gives out.
	if c.dstTop != nil && os.SameFile(info, c.dstTop) {
		return errors.WithMessagef(ErrCopyIntoItself, "source directory %q is the destination", src)
	}

	if err := ensureDir(c.dstRoot, dst, dirPerm(info)); err != nil {
		return err
	}

	if c.dstTop == nil {
		top, err := c.dstRoot.Stat(dst)
		if err != nil {
			return errors.Wrap(err, "failed to stat destination directory")
		}

		c.dstTop = top
	}

	entries, err := fs.ReadDir(c.srcRoot.FS(), src)
	if err != nil {
		return errors.Wrap(err, "failed to read source directory")
	}

	for _, entry := range entries {
		childSrc := path.Join(src, entry.Name())
		childDst := path.Join(dst, entry.Name())

		childInfo, err := c.srcRoot.Lstat(childSrc)
		if err != nil {
			return errors.Wrap(err, "failed to stat directory entry")
		}

		if err := c.entry(childSrc, childDst, childInfo); err != nil {
			return err
		}
	}

	return nil
}

func (c *copier) file(src, dst string, info fs.FileInfo) error {
	if err := ensureParent(c.dstRoot, dst); err != nil {
		return err
	}

	srcFile, err := c.srcRoot.Open(src)
	if err != nil {
		return errors.Wrap(err, "failed to open source file")
	}
	defer srcFile.Close()

	perm := (info.Mode() | c.opts.AddPermission).Perm()

	// The destination is opened as it stands and emptied only once it is known
	// to be another file. Its name says nothing about that: a link in the
	// destination tree, a link the source was reached through or a second name
	// of the same file all lead back to what is being read.
	dstFile, err := c.dstRoot.OpenFile(dst, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return errors.Wrap(err, "failed to create destination file")
	}

	if err := emptyUnlessSame(srcFile, dstFile); err != nil {
		_ = dstFile.Close()
		return errors.WithMessagef(err, "failed to copy %q to %q", src, dst)
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
	if err := c.dstRoot.Chmod(dst, perm); err != nil {
		return errors.Wrap(err, "failed to set destination file mode")
	}

	return nil
}

// emptyUnlessSame truncates dst, unless it is the file src reads from. The two
// are compared as they were opened, so neither can become another file between
// the comparison and the write.
func emptyUnlessSame(src, dst *os.File) error {
	srcInfo, err := src.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat source file")
	}

	dstInfo, err := dst.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat destination file")
	}

	if os.SameFile(srcInfo, dstInfo) {
		return ErrCopySameFile
	}

	// Only a regular file has a length to cut, as with O_TRUNC.
	if !dstInfo.Mode().IsRegular() {
		return nil
	}

	if err := dst.Truncate(0); err != nil {
		return errors.Wrap(err, "failed to truncate destination file")
	}

	return nil
}

func (c *copier) symlink(src, dst string) error {
	switch c.opts.Symlink {
	case SymlinkSkip:
		return nil
	case SymlinkDeep:
		target, err := c.srcRoot.Stat(src)
		if err != nil {
			return errors.Wrap(err, "failed to stat symlink target")
		}

		switch {
		case target.IsDir():
			return c.dir(src, dst, target)
		case target.Mode().IsRegular():
			return c.file(src, dst, target)
		default:
			return nil
		}
	default: // SymlinkShallow
		linkTarget, err := c.srcRoot.Readlink(src)
		if err != nil {
			return errors.Wrap(err, "failed to read symlink")
		}

		if err := ensureParent(c.dstRoot, dst); err != nil {
			return err
		}

		_ = c.dstRoot.Remove(dst)

		if err := c.dstRoot.Symlink(linkTarget, dst); err != nil {
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
