// Package fsutil provides path confinement and recursive copy primitives built
// on top of Go's os.Root. Every file operation that handles a caller-supplied
// path should resolve it with RootRel and then act through an *os.Root opened
// at the work directory: os.Root resolves each path component through the
// directory fd and refuses symlink / ".." escapes without TOCTOU races, on
// both Linux and Windows.
//
// The recursive copy reproduces the default behaviour of the recursive-copy
// library it replaced (shallow symlinks, source permissions preserved, merge
// into existing directories) with one deliberate difference: sockets, fifos
// and device nodes are skipped instead of erroring, because game-server work
// directories legitimately contain unix sockets.
package fsutil

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

// ErrPathOutsideRoot is returned when a caller-supplied path cannot be
// expressed as a location inside the work directory. The message is kept
// stable because API clients and existing tests match on it.
var ErrPathOutsideRoot = errors.New("path is outside work directory")

// RootRel normalizes a caller-supplied path into a clean, slash-separated,
// root-relative name suitable both for *os.Root methods and for root.FS()
// (io/fs) traversal.
//
// "" and "." map to ".". Volume names and leading separators are stripped so
// an absolute-looking path from a client is treated as relative to the work
// directory (this preserves historical behaviour and the Windows
// double-volume defense). os.Root remains the real security boundary; this
// only rejects lexically obvious escapes early with a friendly error.
func RootRel(p string) (string, error) {
	p = filepath.FromSlash(p)

	if vol := filepath.VolumeName(p); vol != "" {
		p = p[len(vol):]
	}

	p = strings.TrimLeft(p, `/\`)
	p = path.Clean(filepath.ToSlash(p))

	if p == "" || p == "." {
		return ".", nil
	}

	if !fs.ValidPath(p) {
		return "", ErrPathOutsideRoot
	}

	return p, nil
}
