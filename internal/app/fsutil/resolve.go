package fsutil

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pkg/errors"
)

// maxResolveHops bounds symlink expansion during Resolve. It matches os.Root's
// own rootMaxSymlinks, so a path accepted here is one os.Root resolves too.
const maxResolveHops = 8

// LeafMode selects what Resolve does with a symlink in the final component.
type LeafMode int

const (
	// FollowLeaf expands a final symlink, for operations whose os.Root
	// primitive follows one: Open, Stat, ReadDir, WriteFile, Chmod, Chtimes.
	FollowLeaf LeafMode = iota
	// NoFollowLeaf keeps a final symlink as the object of the operation, the
	// way Lstat, Remove, Rename and Lchown treat it.
	NoFollowLeaf
)

// ResolveOption configures a Resolver.
type ResolveOption func(*Resolver)

// WithAllowedSymlinkTargets lists directories outside the work directory that
// symlinks under it may lead into (the allowed_symlink_targets config key).
func WithAllowedSymlinkTargets(dirs []string) ResolveOption {
	return func(r *Resolver) {
		for _, dir := range dirs {
			if dir == "" {
				continue
			}

			r.roots = append(r.roots, newTrustedRoot(dir))
		}
	}
}

// Resolver places request paths inside the *os.Root they must be accessed
// through.
//
// Without allowed symlink targets it is exactly os.OpenRoot(work dir) plus
// RootRel. With them, it walks the path one component at a time through the
// current root, expanding every symlink itself, and re-anchors the walk at an
// allowed directory when a link leads there. The daemon thereby never opens a
// path an unprivileged user can influence by absolute name: OpenRoot is only
// ever called on the work directory or a configured target, and every other
// component is reached fd-relative through os.Root. Whatever the returned
// root is later asked to do is resolved by os.Root once more, so a component
// swapped after the walk stays confined to the root it was walked in.
//
// Roots are opened per call because the work directory is provisioned after
// the daemon starts and may not exist yet.
type Resolver struct {
	roots []trustedRoot // the work directory first, then the allowed targets
}

// trustedRoot is a directory the daemon may open by absolute name.
type trustedRoot struct {
	dir       string   // as configured; what OpenRoot is called with
	spellings []string // dir and, when it differs, dir with its own symlinks resolved
	work      bool
}

func newTrustedRoot(dir string) trustedRoot {
	dir = filepath.Clean(dir)
	tr := trustedRoot{dir: dir, spellings: []string{dir}}

	if resolved, err := filepath.EvalSymlinks(dir); err == nil && !samePath(resolved, dir) {
		tr.spellings = append(tr.spellings, resolved)
	}

	return tr
}

func (tr *trustedRoot) matches(abs string) bool {
	for _, s := range tr.spellings {
		if samePath(s, abs) {
			return true
		}
	}

	return false
}

func (tr *trustedRoot) open() (*os.Root, error) {
	root, err := os.OpenRoot(tr.dir)
	if err != nil {
		if tr.work {
			return nil, errors.Wrap(err, "work directory unavailable")
		}

		// Same marker as the work directory: the API reports it as a node
		// problem rather than a client mistake.
		return nil, errors.Wrapf(err, "work directory unavailable: allowed symlink target %q", tr.dir)
	}

	return root, nil
}

// NewResolver creates a Resolver anchored at workDir.
func NewResolver(workDir string, opts ...ResolveOption) *Resolver {
	work := newTrustedRoot(workDir)
	work.work = true

	r := &Resolver{roots: []trustedRoot{work}}
	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Resolved is a request path placed inside the root it must be accessed through.
type Resolved struct {
	// Root is opened at Anchor. The caller owns it and closes it.
	Root *os.Root
	// Rel is the path inside Root, in the form RootRel produces.
	Rel string
	// Anchor is the absolute directory Root was opened at: the work directory
	// or one of the allowed symlink targets.
	Anchor string
}

func (r *Resolved) Close() error {
	return r.Root.Close()
}

// SameAnchor reports whether both paths live in one root, so that a
// single-root primitive such as Rename can take them together.
func (r *Resolved) SameAnchor(other *Resolved) bool {
	return samePath(r.Anchor, other.Anchor)
}

// SymlinkRefusedError reports a symlink whose target lies outside the work
// directory and every allowed_symlink_targets entry. It unwraps to
// ErrPathOutsideRoot, whose text the API maps to a client error.
type SymlinkRefusedError struct {
	Link   string // absolute path of the link
	Target string // its literal target
}

func (e *SymlinkRefusedError) Error() string {
	return fmt.Sprintf(
		"symlink %q -> %q is not under work_path or any allowed_symlink_targets entry: %s",
		e.Link, e.Target, ErrPathOutsideRoot.Error(),
	)
}

func (e *SymlinkRefusedError) Unwrap() error {
	return ErrPathOutsideRoot
}

// Resolve resolves a caller-supplied path. See Resolver for the model.
func (r *Resolver) Resolve(p string, leaf LeafMode) (*Resolved, error) {
	rel, err := RootRel(p)
	if err != nil {
		return nil, err
	}

	work := &r.roots[0]

	root, err := work.open()
	if err != nil {
		return nil, err
	}

	if len(r.roots) == 1 {
		return &Resolved{Root: root, Rel: rel, Anchor: work.dir}, nil
	}

	w := &walker{roots: r.roots, leaf: leaf, root: root, anchor: work, cur: "."}

	res, err := w.walk(strings.Split(rel, "/"))
	if err != nil {
		w.close()

		return nil, err
	}

	return res, nil
}

// walker is one Resolve in progress. Inside a root it holds the root and the
// position in it; after a link led outside every root it only tracks the
// lexical absolute position until that position is a root again.
type walker struct {
	roots []trustedRoot
	leaf  LeafMode

	root   *os.Root     // nil while outside every root
	anchor *trustedRoot // the directory root is opened at
	cur    string       // position inside anchor, "." at its top
	abs    string       // position while outside every root
	hops   int

	lastLink   string // absolute path of the last expanded link, for the refusal
	lastTarget string
}

func (w *walker) walk(queue []string) (*Resolved, error) {
	for len(queue) > 0 {
		comp := queue[0]
		queue = queue[1:]

		if w.root == nil {
			if err := w.stepOutside(comp); err != nil {
				return nil, err
			}

			continue
		}

		switch comp {
		case "", ".":
			continue
		case "..":
			if err := w.stepUp(); err != nil {
				return nil, err
			}

			continue
		}

		next := comp
		if w.cur != "." {
			next = path.Join(w.cur, comp)
		}

		info, err := w.root.Lstat(next)
		if err != nil {
			// What is left cannot be inspected; the operation reports what is
			// wrong with it (a missing mkdir target is not wrong at all).
			return w.finish(next, queue)
		}

		if !isLink(info) || (len(queue) == 0 && w.leaf == NoFollowLeaf) {
			w.cur = next

			continue
		}

		target, err := w.root.Readlink(next)
		if err != nil {
			if info.Mode()&fs.ModeSymlink == 0 {
				// A Windows reparse point that is not a link (e.g. a dedup or
				// OneDrive placeholder) is an ordinary entry.
				w.cur = next

				continue
			}

			return nil, errors.Wrapf(err, "failed to read symlink %q", next)
		}

		w.hops++
		if w.hops > maxResolveHops {
			return nil, errors.Errorf("too many levels of symbolic links at %q", next)
		}

		w.lastLink = filepath.Join(w.anchor.dir, filepath.FromSlash(next))
		w.lastTarget = target

		parts, err := w.enter(target, path.Dir(next))
		if err != nil {
			return nil, err
		}

		queue = append(parts, queue...)
	}

	if w.root == nil {
		return nil, w.refuse()
	}

	return &Resolved{Root: w.root, Rel: w.cur, Anchor: w.anchor.dir}, nil
}

// enter positions the walk where target starts resolving from — the link's
// own directory for a relative target, the filesystem root for an absolute
// one — and returns the target's components.
func (w *walker) enter(target, linkDir string) ([]string, error) {
	if filepath.IsAbs(target) {
		vol := filepath.VolumeName(target)

		if err := w.leave(vol + string(filepath.Separator)); err != nil {
			return nil, err
		}

		return strings.Split(filepath.ToSlash(target[len(vol):]), "/"), nil
	}

	if runtime.GOOS == "windows" && (filepath.VolumeName(target) != "" || strings.HasPrefix(target, `\`) ||
		strings.HasPrefix(target, "/")) {
		// A drive-relative ("C:x") or rooted ("\x") target resolves against
		// process state rather than the link's directory.
		return nil, w.refuse()
	}

	w.cur = linkDir

	return strings.Split(filepath.ToSlash(target), "/"), nil
}

// stepUp applies ".." inside a root. At the top it leaves the root: the
// parent of an anchor is not necessarily a trusted directory.
func (w *walker) stepUp() error {
	if w.cur != "." {
		w.cur = path.Dir(w.cur)

		return nil
	}

	return w.leave(filepath.Dir(w.anchor.dir))
}

// leave closes the current root and continues lexically from abs.
func (w *walker) leave(abs string) error {
	w.close()
	w.anchor = nil
	w.cur = ""
	w.abs = abs

	return w.tryAnchor()
}

// stepOutside consumes one component while outside every root. Nothing is
// opened or inspected out there: the path is only followed on paper until
// it is a trusted directory again.
func (w *walker) stepOutside(comp string) error {
	switch comp {
	case "", ".":
		return nil
	case "..":
		w.abs = filepath.Dir(w.abs)
	default:
		w.abs = filepath.Join(w.abs, comp)
	}

	return w.tryAnchor()
}

// tryAnchor re-enters a root when the lexical position is one of them.
func (w *walker) tryAnchor() error {
	for i := range w.roots {
		if !w.roots[i].matches(w.abs) {
			continue
		}

		root, err := w.roots[i].open()
		if err != nil {
			return err
		}

		w.root = root
		w.anchor = &w.roots[i]
		w.cur = "."
		w.abs = ""

		return nil
	}

	return nil
}

// finish appends the components that could not be inspected to the position
// reached. They are cleaned lexically so a ".." from a link target cannot make
// the result point above the root.
func (w *walker) finish(next string, rest []string) (*Resolved, error) {
	rel := path.Join(append([]string{next}, rest...)...)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, ErrPathOutsideRoot
	}

	return &Resolved{Root: w.root, Rel: rel, Anchor: w.anchor.dir}, nil
}

func (w *walker) refuse() error {
	return &SymlinkRefusedError{Link: w.lastLink, Target: w.lastTarget}
}

func (w *walker) close() {
	if w.root != nil {
		_ = w.root.Close()
		w.root = nil
	}
}

// isLink reports whether an entry has to be read with Readlink. Windows
// reports junctions (mklink /J), the usual way to put a server directory on
// another drive there, as irregular reparse points rather than symlinks.
func isLink(info fs.FileInfo) bool {
	mode := info.Mode()
	if mode&fs.ModeSymlink != 0 {
		return true
	}

	return runtime.GOOS == "windows" && mode&fs.ModeIrregular != 0
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)

	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}

	return a == b
}
