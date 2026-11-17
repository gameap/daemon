package fsutil

import (
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/pkg/errors"
)

// ErrPathOutsideConfinement is returned when a request names a confinement
// root and one of its paths does not lie inside it. The message is kept stable
// because the panel matches on it.
var ErrPathOutsideConfinement = errors.New("path is outside its confinement root")

// Target says what an operation does with the path it resolves. It only
// matters for a path that is itself a game server directory.
type Target int

const (
	// TargetEntry is for operations that act on the named entry as such: they
	// stat, remove, rename, chmod or create it, or read and write it as a
	// file. The entry belongs to the directory holding it, so a game server
	// directory named this way is reached from outside and can still be
	// removed or renamed.
	TargetEntry Target = iota
	// TargetTree is for operations that work inside the named directory: they
	// list or walk it, or create entries below it. A game server directory
	// named this way confines the operation.
	TargetTree
)

// ServerDirLister reports the directories of the game servers the node hosts,
// relative to the work directory.
type ServerDirLister interface {
	ServerDirs() []string
}

// ConfineOption configures a Confiner.
type ConfineOption func(*Confiner)

// WithServerDirs makes every game server directory a boundary for the paths
// below it.
func WithServerDirs(lister ServerDirLister) ConfineOption {
	return func(c *Confiner) {
		c.serverDirs = lister
	}
}

// WithRoot sets the confinement root a request names itself. It is a boundary
// of its own next to the game server directories, never a replacement for
// them. An empty root names nothing.
func WithRoot(root string) ConfineOption {
	return func(c *Confiner) {
		c.root = root
	}
}

// Confiner places request paths inside the directory they must not leave.
//
// The work directory is shared by every game server of the node, and the files
// of one server are written by its tenant. An os.Root opened at the work
// directory keeps a request inside the node, but it follows a symlink from one
// server into another, into the steamcmd or tools directory, or into the files
// the daemon keeps for itself. Confiner therefore opens the root at the
// directory the request belongs to. Two things draw that boundary, and both
// apply:
//
//   - the game server directories the daemon knows (WithServerDirs): a path
//     below one of them is confined to it, which covers panels that do not
//     send a root;
//   - the root the request names (WithRoot), which the panel sets to the game
//     server directory for everything it does on behalf of that server: a path
//     outside of it is refused, and it confines even where the daemon has not
//     been told about the server yet.
//
// A request root thus only narrows what the daemon derives on its own. A path
// that neither of them covers is confined to the work directory.
//
// Boundaries nest, and each is reached through the one enclosing it, so it is
// opened fd-relative like any other component: an absolute or escaping symlink
// in its own path is refused, including one the tenant of an outer game server
// puts in place of a directory nested in its own.
//
// A boundary is a directory, not a name. A path may spell a game server
// directory or the request root unlike the daemon does: in another letter case
// on a case-insensitive volume, by a Windows short name, through a link
// standing next to it. Such a path is placed by the directory it leads to.
//
// Resolve places the path a request names, and nothing below it. An operation
// that walks a tree from above the game server directories, as a request
// acting for the node rather than for one server can, crosses into them
// through the root it started with unless it asks IsServerDir about the
// directories it enters.
type Confiner struct {
	workDir    string
	serverDirs ServerDirLister
	root       string

	identify   sync.Once
	identities []os.FileInfo
}

// NewConfiner creates a Confiner anchored at workDir.
func NewConfiner(workDir string, opts ...ConfineOption) *Confiner {
	c := &Confiner{workDir: workDir}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Confined is a request path placed inside the root it must be accessed through.
type Confined struct {
	// Root is opened at Scope. The caller owns it and closes it.
	Root *os.Root
	// Rel is the path inside Root, in the form RootRel produces.
	Rel string
	// Scope is the directory Root is opened at, relative to the work directory
	// ("." for the work directory itself).
	Scope string
}

func (c *Confined) Close() error {
	return c.Root.Close()
}

// SameRoot reports whether both paths live in one root, so that a single-root
// primitive such as os.Root.Rename can take them together. The roots are
// compared as directories: Scope is spelled the way each request path spells
// it, and two spellings can name one directory or two.
func (c *Confined) SameRoot(other *Confined) bool {
	own, err := c.Root.Stat(".")
	if err != nil {
		return false
	}

	theirs, err := other.Root.Stat(".")
	if err != nil {
		return false
	}

	return os.SameFile(own, theirs)
}

// IsServerDir reports whether info describes a game server directory. A walk
// that starts above the game server directories uses it to notice the ones it
// enters, which no request path names.
func (c *Confiner) IsServerDir(info os.FileInfo) bool {
	if c.serverDirs == nil || !info.IsDir() {
		return false
	}

	c.identify.Do(func() {
		root, err := os.OpenRoot(c.workDir)
		if err != nil {
			return
		}
		defer root.Close()

		for _, serverDir := range c.serverDirs.ServerDirs() {
			dir, err := RootRel(serverDir)
			if err != nil || dir == "." {
				continue
			}

			if known, err := root.Stat(dir); err == nil && known.IsDir() {
				c.identities = append(c.identities, known)
			}
		}
	})

	for _, known := range c.identities {
		if os.SameFile(info, known) {
			return true
		}
	}

	return false
}

// Resolve places a caller-supplied path. See Confiner for the model.
func (c *Confiner) Resolve(p string, target Target) (*Confined, error) {
	rel, err := RootRel(p)
	if err != nil {
		return nil, err
	}

	boundaries, err := c.boundaries(rel, target)
	if err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(c.workDir)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}

	scope := "."

	for _, dir := range boundaries {
		next, err := root.OpenRoot(relativeTo(dir, scope))
		if err != nil {
			// A boundary that is not on disk yet holds nothing a tenant could
			// have planted: whatever the operation creates below it is created
			// by the daemon, from the enclosing root.
			if errors.Is(err, os.ErrNotExist) {
				break
			}

			_ = root.Close()

			return nil, errors.Wrapf(err, "failed to open confinement directory %q", dir)
		}

		_ = root.Close()
		root, scope = next, dir
	}

	return &Confined{Root: root, Rel: relativeTo(rel, scope), Scope: scope}, nil
}

// boundaries returns the directories that confine rel, outermost first. Each
// is spelled the way rel spells it, so the directory opened is the one the
// request names even where the file system compares names loosely.
func (c *Confiner) boundaries(rel string, target Target) ([]string, error) {
	var lengths []int

	if c.root != "" {
		root, err := RootRel(c.root)
		if err != nil {
			return nil, err
		}

		// The work directory already confines every path.
		if root != "." {
			n := leadingLen(rel, root)
			if n == 0 || !c.sameDir(rel[:n], root) {
				return nil, ErrPathOutsideConfinement
			}

			lengths = append(lengths, n)
		}
	}

	if c.serverDirs != nil {
		lengths = c.serverBoundaries(rel, target, lengths)
	}

	// Every match is a leading part of rel, so ordering by length puts an
	// enclosing directory before the one nested in it.
	slices.Sort(lengths)
	lengths = slices.Compact(lengths)

	dirs := make([]string, 0, len(lengths))
	for _, n := range lengths {
		dirs = append(dirs, rel[:n])
	}

	return dirs, nil
}

// serverBoundaries adds the leading parts of rel that are game server
// directories to lengths.
func (c *Confiner) serverBoundaries(rel string, target Target, lengths []int) []int {
	// Leading parts as deep as a game server directory but spelled unlike it.
	var loose []int

	for _, serverDir := range c.serverDirs.ServerDirs() {
		dir, err := RootRel(serverDir)
		if err != nil || dir == "." {
			continue
		}

		n := leadingLen(rel, dir)
		// An entry named as such belongs to the directory holding it.
		if n == 0 || (n == len(rel) && target != TargetTree) {
			continue
		}

		if rel[:n] == dir {
			lengths = append(lengths, n)
		} else {
			loose = append(loose, n)
		}
	}

	slices.Sort(loose)

	// The usual request spells its game server directory the way the daemon
	// knows it and is settled above without touching the disk. What is left
	// may still be one under another spelling, which only the disk can tell.
	for _, n := range slices.Compact(loose) {
		if slices.Contains(lengths, n) {
			continue
		}

		if info, err := c.stat(rel[:n]); err == nil && c.IsServerDir(info) {
			lengths = append(lengths, n)
		}
	}

	return lengths
}

// leadingLen returns the length of the leading part of rel that is as deep as
// dir, or 0 when rel is not that deep.
func leadingLen(rel, dir string) int {
	if rel == "." {
		return 0
	}

	end := 0

	for range strings.Count(dir, "/") + 1 {
		if end == len(rel) {
			return 0
		}

		if end > 0 {
			end++
		}

		next := strings.IndexByte(rel[end:], '/')
		if next < 0 {
			end = len(rel)
		} else {
			end += next
		}
	}

	return end
}

// sameDir reports whether two names lead to one directory of the work
// directory. Names spelled alike do without a lookup, which also covers a
// directory that is not on disk yet.
func (c *Confiner) sameDir(a, b string) bool {
	if a == b {
		return true
	}

	infoA, err := c.stat(a)
	if err != nil || !infoA.IsDir() {
		return false
	}

	infoB, err := c.stat(b)
	if err != nil {
		return false
	}

	return os.SameFile(infoA, infoB)
}

// stat describes what a name relative to the work directory leads to.
func (c *Confiner) stat(name string) (os.FileInfo, error) {
	root, err := os.OpenRoot(c.workDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	return root.Stat(name)
}

// relativeTo expresses rel, which is base or lies below it, relative to base.
func relativeTo(rel, base string) string {
	if base == "." {
		return rel
	}

	if len(rel) <= len(base) {
		return "."
	}

	return rel[len(base)+1:]
}
