package fsutil

import (
	"cmp"
	"os"
	"path"
	"path/filepath"
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
	// named this way confines the operation, and so does one a link in the
	// last component leads into: the path is then placed where the link ends.
	// A link that crosses no boundary is left to the operation as it is named.
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
// A boundary is a directory, not a name. A path can reach a game server
// directory or the request root without spelling it the way the daemon does:
// in another letter case on a case-insensitive volume, by a Windows short
// name, through a link standing anywhere above it, or through a link that
// leads into the middle of it. A path that does not spell a game server
// directory out is therefore walked down from the work directory one component
// at a time. Every directory on the way is compared with the boundaries as
// they are on disk at that moment, and every link is replaced by what it leads
// to, so the path ends up in the directory it really lies in. A boundary found
// this way is compared as it was opened: the directory the request goes on in
// is the one that was checked, whatever its name leads to a moment later.
//
// A path that does spell a game server directory the way the daemon knows it
// is placed by that spelling alone, without a lookup, and os.Root keeps the
// rest of it inside. The walk would add nothing there unless game server
// directories are nested in one another and the path reaches a second one of
// them under another name.
//
// Resolve places the path a request names, and nothing below it. An operation
// that walks a tree from above the game server directories, as a request
// acting for the node rather than for one server can, crosses into them
// through the root it started with unless it asks IsServerDir about the
// directories it enters and Resolve about the links it follows.
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
	// InServerDir is set when Root stands at a game server directory or below
	// one. Whatever is reached through such a root stays in that directory.
	InServerDir bool
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
//
// The game server directories are looked up on every call. A walk lasts long
// enough for one of them to be removed and created again, and the directory
// found under its name then is not the one that stood there when the walk
// began.
func (c *Confiner) IsServerDir(info os.FileInfo) bool {
	names := c.serverNames()
	if len(names) == 0 || !info.IsDir() {
		return false
	}

	work, err := os.OpenRoot(c.workDir)
	if err != nil {
		return false
	}
	defer work.Close()

	return isOneOf(info, onDisk(work, names))
}

// Resolve places a caller-supplied path. See Confiner for the model.
func (c *Confiner) Resolve(p string, target Target) (*Confined, error) {
	rel, err := RootRel(p)
	if err != nil {
		return nil, err
	}

	b, err := c.bounds()
	if err != nil {
		return nil, err
	}

	work, err := os.OpenRoot(c.workDir)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}

	if named, settled := b.named(rel, target); settled {
		return openNamed(work, rel, named)
	}

	return walk(work, b, rel, target)
}

// bounds are the directories one request is confined by, spelled the way the
// daemon knows them.
type bounds struct {
	servers []string
	// root is the request root. It is empty when the request names none, or
	// names the work directory, which confines every path already.
	root string
}

func (c *Confiner) bounds() (bounds, error) {
	b := bounds{servers: c.serverNames()}

	if c.root == "" {
		return b, nil
	}

	root, err := RootRel(c.root)
	if err != nil {
		return bounds{}, err
	}

	if root != "." {
		b.root = root
	}

	return b, nil
}

// serverNames returns the game server directories that can be a boundary: a
// name the work directory cannot hold, or the work directory itself, is none.
func (c *Confiner) serverNames() []string {
	if c.serverDirs == nil {
		return nil
	}

	var names []string

	for _, serverDir := range c.serverDirs.ServerDirs() {
		if dir, err := RootRel(serverDir); err == nil && dir != "." {
			names = append(names, dir)
		}
	}

	return names
}

// boundary is a leading part of a request path that names a directory the
// path is confined by.
type boundary struct {
	end    int
	server bool
}

// named returns the boundaries rel spells out, outermost first, and whether
// they settle where rel belongs: it names a game server directory, if the
// daemon knows any, and lies in the request root, if the request has one.
func (b bounds) named(rel string, target Target) ([]boundary, bool) {
	var found []boundary

	inRoot := b.root == ""
	if !inRoot && below(rel, b.root) {
		inRoot = true

		found = append(found, boundary{end: len(b.root), server: slices.Contains(b.servers, b.root)})
	}

	inServer := len(b.servers) == 0

	for _, dir := range b.servers {
		// An entry named as such belongs to the directory holding it.
		if !below(rel, dir) || (rel == dir && target != TargetTree) {
			continue
		}

		inServer = true

		found = append(found, boundary{end: len(dir), server: true})
	}

	// Every boundary is a leading part of rel, so ordering by length puts an
	// enclosing directory before the one nested in it.
	slices.SortFunc(found, func(x, y boundary) int { return cmp.Compare(x.end, y.end) })

	found = slices.CompactFunc(found, func(x, y boundary) bool { return x.end == y.end })

	return found, inRoot && inServer
}

// openNamed opens the boundaries rel spells out, each from inside the one
// before it.
func openNamed(work *os.Root, rel string, named []boundary) (*Confined, error) {
	placed := &Confined{Root: work, Scope: "."}

	for _, bound := range named {
		dir := rel[:bound.end]

		next, err := placed.Root.OpenRoot(relativeTo(dir, placed.Scope))
		if err != nil {
			// A boundary that is not on disk yet holds nothing a tenant could
			// have planted: whatever the operation creates below it is created
			// by the daemon, from the enclosing root.
			if errors.Is(err, os.ErrNotExist) {
				break
			}

			_ = placed.Root.Close()

			return nil, errors.Wrapf(err, "failed to open confinement directory %q", dir)
		}

		_ = placed.Root.Close()

		placed.Root, placed.Scope = next, dir
		placed.InServerDir = placed.InServerDir || bound.server
	}

	placed.Rel = relativeTo(rel, placed.Scope)

	return placed, nil
}

// maxLinkHops bounds the links one path is led through while it is placed.
// os.Root gives up on a single name long before that; the limit is for a tree
// that keeps changing underneath the walk.
const maxLinkHops = 64

// placer walks a request path down from the work directory and moves into
// every boundary the path passes.
type placer struct {
	// servers and reqRoot are the boundaries as they are on disk. reqRoot is
	// nil when the request names no root or the root is not there.
	servers []os.FileInfo
	reqRoot os.FileInfo

	// root is the innermost boundary reached, opened at scope.
	root  *os.Root
	scope string
	// at is where the walk stands inside root. No link is left in it.
	at string

	inRoot   bool
	inServer bool
	hops     int
}

// walk places a path that does not spell its boundaries out.
func walk(work *os.Root, b bounds, rel string, target Target) (*Confined, error) {
	p := &placer{root: work, scope: ".", at: "."}

	var names []string

	if rel != "." {
		names = strings.Split(rel, "/")
		p.servers = onDisk(work, b.servers)

		if b.root != "" {
			if dirs := onDisk(work, []string{b.root}); len(dirs) > 0 {
				p.reqRoot = dirs[0]
			}
		}
	}

	for i, name := range names {
		moved, err := p.step(name, i == len(names)-1, target)
		if err != nil {
			_ = p.root.Close()

			return nil, err
		}

		if !moved {
			p.at = path.Join(p.at, strings.Join(names[i:], "/"))

			break
		}
	}

	// A request root that is spelled out but not on disk yet has nothing in it
	// to keep the request from, like any other boundary that is not there.
	if b.root != "" && !p.inRoot && !below(rel, b.root) {
		_ = p.root.Close()

		return nil, ErrPathOutsideConfinement
	}

	return &Confined{Root: p.root, Rel: p.at, Scope: p.scope, InServerDir: p.inServer}, nil
}

// step takes the walk one component of the request path further and reports
// whether it got there. Where it did not, the rest of the path is left the way
// the request spells it, for os.Root to resolve from where the walk stands.
func (p *placer) step(name string, last bool, target Target) (bool, error) {
	// No boundary is left to be found.
	if len(p.servers) == 0 && (p.reqRoot == nil || p.inRoot) {
		return false, nil
	}

	switch {
	case !last:
		return p.take(name)
	case target == TargetTree:
		return p.tree(name)
	default:
		return p.entry(name)
	}
}

// take moves into name, an entry of the directory the walk stands in, with
// every link it leads through followed by hand. It reports false, and has
// moved nowhere, when name itself is nothing to move into: it is missing, or a
// link os.Root does not follow from here. What goes wrong further along a link
// is an error: the path leads somewhere, and not to a place it may be in.
func (p *placer) take(name string) (bool, error) {
	queue := []string{name}
	linked := false

	for len(queue) > 0 {
		part := queue[0]
		queue = queue[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			// Only a link that has led into a boundary gets here: one that
			// leaves the root it starts in is not followed in the first place.
			if p.at == "." {
				return false, errors.Errorf("failed to follow link %q: it leads back out of %q", name, p.scope)
			}

			p.at = path.Dir(p.at)

			continue
		}

		next := path.Join(p.at, part)

		info, err := p.root.Lstat(next)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			// What os.Root does not follow from here is not followed by hand
			// either: a dangling link, an absolute one, and above all one that
			// leads out of the root, so that no boundary is left this way.
			_, err = p.root.Stat(next)
		}

		if err != nil {
			if !linked {
				return false, nil
			}

			return false, errors.Wrapf(err, "failed to follow link %q", name)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := p.readLink(next)
			if err != nil {
				return false, errors.WithMessagef(err, "failed to follow link %q", name)
			}

			queue = append(strings.Split(filepath.ToSlash(target), "/"), queue...)
			linked = true

			continue
		}

		if !info.IsDir() {
			p.at = next

			continue
		}

		// "dir/.." is the directory the walk stands in already: a path that
		// only passes a boundary by has not entered it.
		if len(queue) > 0 && queue[0] == ".." {
			queue = queue[1:]

			continue
		}

		p.at = next

		if err := p.cross(info); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (p *placer) readLink(name string) (string, error) {
	if p.hops++; p.hops > maxLinkHops {
		return "", errors.Errorf("more than %d links on the way", maxLinkHops)
	}

	target, err := p.root.Readlink(name)
	if err != nil {
		return "", errors.Wrapf(err, "failed to read link %q", name)
	}

	return target, nil
}

// tree moves into the last component when the operation works inside it and a
// boundary lies on the way there. Otherwise the component is left as named: an
// operation that meets a link there deals with the link.
func (p *placer) tree(name string) (bool, error) {
	root, at := p.root, p.at

	moved, err := p.take(name)
	if err != nil || !moved {
		return false, err
	}

	if p.root == root {
		p.at = at

		return false, nil
	}

	return true, nil
}

// entry leaves the last component to the operation, which acts on it as an
// entry of the directory the walk stands in. Only the request root is moved
// into: named as an entry, it still is the directory the request is kept in.
func (p *placer) entry(name string) (bool, error) {
	if p.reqRoot == nil || p.inRoot {
		return false, nil
	}

	info, err := p.root.Stat(path.Join(p.at, name))
	if err != nil || !os.SameFile(info, p.reqRoot) {
		return false, nil
	}

	moved, err := p.take(name)
	if err != nil || !moved {
		return false, err
	}

	if !p.inRoot {
		return false, errors.Errorf("request root named by %q was replaced while the path was being placed", name)
	}

	return true, nil
}

// cross moves the walk into the directory it stands at when that directory is
// a boundary. info describes the directory as it was found by name.
func (p *placer) cross(info os.FileInfo) error {
	isRoot := p.reqRoot != nil && os.SameFile(info, p.reqRoot)
	isServer := isOneOf(info, p.servers)

	if !isRoot && !isServer {
		return nil
	}

	dir := path.Join(p.scope, p.at)

	inner, err := p.root.OpenRoot(p.at)
	if err != nil {
		return errors.Wrapf(err, "failed to open confinement directory %q", dir)
	}

	// The name was looked at before it was opened and may lead elsewhere by
	// now. What counts is the directory in hand: the request goes on in it
	// only if it is the one that was found to be the boundary.
	opened, err := inner.Stat(".")
	if err != nil {
		_ = inner.Close()

		return errors.Wrapf(err, "failed to stat confinement directory %q", dir)
	}

	if !os.SameFile(opened, info) {
		_ = inner.Close()

		return errors.Errorf("confinement directory %q was replaced while it was being opened", dir)
	}

	_ = p.root.Close()

	p.root, p.scope, p.at = inner, dir, "."
	p.inRoot = p.inRoot || isRoot
	p.inServer = p.inServer || isServer

	return nil
}

// onDisk describes the directories the names lead to at this moment. A name
// that leads to none confines nothing.
func onDisk(work *os.Root, names []string) []os.FileInfo {
	var dirs []os.FileInfo

	for _, name := range names {
		if info, err := work.Stat(name); err == nil && info.IsDir() {
			dirs = append(dirs, info)
		}
	}

	return dirs
}

func isOneOf(info os.FileInfo, dirs []os.FileInfo) bool {
	return slices.ContainsFunc(dirs, func(dir os.FileInfo) bool {
		return os.SameFile(info, dir)
	})
}

// below reports whether rel is dir or lies under it, going by the names.
func below(rel, dir string) bool {
	return rel == dir || strings.HasPrefix(rel, dir+"/")
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
