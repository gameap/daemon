package fsutil

import (
	"os"
	"runtime"
	"slices"
	"strings"

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
type Confiner struct {
	workDir    string
	serverDirs ServerDirLister
	root       string
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
// primitive such as os.Root.Rename can take them together.
func (c *Confined) SameRoot(other *Confined) bool {
	return sameDir(c.Scope, other.Scope)
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

		switch {
		case root == ".":
			// The work directory already confines every path.
		case sameDir(rel, root):
			lengths = append(lengths, len(rel))
		case isBelow(rel, root):
			lengths = append(lengths, len(root))
		default:
			return nil, ErrPathOutsideConfinement
		}
	}

	if c.serverDirs != nil {
		for _, serverDir := range c.serverDirs.ServerDirs() {
			dir, err := RootRel(serverDir)
			if err != nil || dir == "." {
				continue
			}

			switch {
			case isBelow(rel, dir):
				lengths = append(lengths, len(dir))
			case target == TargetTree && sameDir(rel, dir):
				lengths = append(lengths, len(rel))
			}
		}
	}

	// Every match is a prefix of rel, so ordering by length puts an enclosing
	// directory before the one nested in it.
	slices.Sort(lengths)
	lengths = slices.Compact(lengths)

	dirs := make([]string, 0, len(lengths))
	for _, n := range lengths {
		dirs = append(dirs, rel[:n])
	}

	return dirs, nil
}

// isBelow reports whether rel names something strictly inside dir.
func isBelow(rel, dir string) bool {
	if dir == "." {
		return rel != "."
	}

	if len(rel) <= len(dir) || rel[len(dir)] != '/' {
		return false
	}

	return sameDir(rel[:len(dir)], dir)
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

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}

	return a == b
}
