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
// at a time. Every directory on the way is compared with what the names of the
// boundaries lead to, and every link is replaced by what it leads to, so the
// path ends up in the directory it really lies in. A boundary found this way
// is compared as it was opened: the directory the request goes on in is the
// one that was checked, whatever its name leads to a moment later. What the
// names lead to is looked up before the walk and once more after it. A game
// server directory removed and made anew in between is a boundary the walk
// could not tell, so the path is placed again.
//
// A path that does spell a game server directory the way the daemon knows it
// is placed by that spelling alone, without a lookup, and os.Root keeps the
// rest of it inside. That is enough as long as no other game server directory
// lies inside the one the path spells: the tenant of the outer one can lead to
// it under any name, so such a path is walked like the others. Whether one
// game server directory lies inside another is taken from the names the daemon
// knows them by. One it knows only by a name that leads into another through a
// link is not found to be nested.
//
// Resolve places the path a request names, and nothing below it. An operation
// that goes on from there by itself, as packing a tree does, crosses into the
// game server directories below through the root it started with, unless that
// root is Innermost, or it asks IsServerDir about the directories it enters
// and Resolve about the links it follows.
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
	// Innermost is set when Root stands in a game server directory that holds
	// no other one, going by the names the daemon knows them by. Whatever is
	// reached through such a root stays in the game server directory it
	// belongs to. Below any other root a game server directory may lie, with a
	// boundary of its own.
	Innermost bool
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
// The game server directories are looked up on every call, after info was
// taken. A walk lasts long enough for one of them to be removed and created
// again, and the directory found under its name then is not the one that stood
// there when the walk began.
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

	return slices.ContainsFunc(names, func(name string) bool {
		dir := dirAt(work, name)

		return dir != nil && os.SameFile(info, dir)
	})
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
		return b.openNamed(work, rel, named)
	}

	return b.walk(work, rel, target, b.onDisk)
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

// holds reports whether the daemon knows a game server directory by a name
// below dir. Letter case is not told apart, since the volume may not tell it
// apart either: two directories wrongly taken for nested cost a lookup, two
// wrongly taken for apart cost the boundary.
func (b bounds) holds(dir string) bool {
	if dir == "." {
		return len(b.servers) > 0
	}

	return slices.ContainsFunc(b.servers, func(name string) bool {
		return belowFold(name, dir)
	})
}

// belowFold reports whether name lies under dir, going by the names with the
// letter case folded.
func belowFold(name, dir string) bool {
	for {
		outer, dirRest, dirGoesOn := strings.Cut(dir, "/")
		inner, nameRest, nameGoesOn := strings.Cut(name, "/")

		if !strings.EqualFold(outer, inner) {
			return false
		}

		if !dirGoesOn {
			return nameGoesOn
		}

		if !nameGoesOn {
			return false
		}

		dir, name = dirRest, nameRest
	}
}

// named returns the boundaries rel spells out, outermost first, and whether
// they settle where rel belongs: it names a game server directory, if the
// daemon knows any, with no other one inside it, and lies in the request root,
// if the request has one.
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

	if !inRoot || !inServer {
		return nil, false
	}

	// A game server directory inside the innermost one named is reached under
	// whatever name the tenant of the outer one gives it. The spelling of rel
	// does not show whether it passes that way.
	if len(found) > 0 && b.holds(rel[:found[len(found)-1].end]) {
		return nil, false
	}

	return found, true
}

// openNamed opens the boundaries rel spells out, each from inside the one
// before it.
func (b bounds) openNamed(work *os.Root, rel string, named []boundary) (*Confined, error) {
	placed := &Confined{Root: work, Scope: "."}
	inServer := false

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
		inServer = inServer || bound.server
	}

	placed.Rel = relativeTo(rel, placed.Scope)
	// The boundary reached is not the innermost one named when that one is
	// not on disk, and the one enclosing it can hold more.
	placed.Innermost = inServer && !b.holds(placed.Scope)

	return placed, nil
}

// maxLinkHops bounds the links one path is led through while it is placed.
// os.Root gives up on a single name long before that; the limit is for a tree
// that keeps changing underneath the walk.
const maxLinkHops = 64

// maxPlacements bounds how often one path is placed anew because the
// boundaries changed underneath the walk.
const maxPlacements = 4

// landmarks are the boundaries as they are on disk at one moment: what the
// names the daemon knows them by lead to. A name that leads to no directory
// has nil for it, and confines nothing.
type landmarks struct {
	// servers has an entry for every name in bounds.servers, in that order.
	servers []os.FileInfo
	root    os.FileInfo
}

func (b bounds) onDisk(work *os.Root) landmarks {
	found := landmarks{servers: make([]os.FileInfo, len(b.servers))}

	for i, name := range b.servers {
		found.servers[i] = dirAt(work, name)
	}

	if b.root != "" {
		found.root = dirAt(work, b.root)
	}

	return found
}

func (l landmarks) hasServers() bool {
	return slices.ContainsFunc(l.servers, func(dir os.FileInfo) bool { return dir != nil })
}

// same reports whether every name leads to the directory it leads to in other.
func (l landmarks) same(other landmarks) bool {
	return sameDir(l.root, other.root) && slices.EqualFunc(l.servers, other.servers, sameDir)
}

func sameDir(x, y os.FileInfo) bool {
	if x == nil || y == nil {
		return x == nil && y == nil
	}

	return os.SameFile(x, y)
}

func dirAt(work *os.Root, name string) os.FileInfo {
	if info, err := work.Stat(name); err == nil && info.IsDir() {
		return info
	}

	return nil
}

// walk places a path that does not spell its boundaries out, and takes work
// over. look describes the boundaries on disk.
//
// The walk tells a boundary by what look found before it began. A directory
// made under the name of a boundary since then is passed like any other, and a
// link in it is followed through the root enclosing it. The names are therefore
// looked up again once the path is placed, and the placement stands only if
// they still lead where they did: every directory the walk passed was then
// compared with what the names led to after the walk had seen it.
func (b bounds) walk(work *os.Root, rel string, target Target, look func(*os.Root) landmarks) (*Confined, error) {
	if rel == "." {
		placed, err := b.place(work, landmarks{}, rel, target)
		if err != nil {
			_ = work.Close()
		}

		return placed, err
	}

	seen := look(work)

	for range maxPlacements {
		placed, err := b.place(work, seen, rel, target)

		now := look(work)
		if seen.same(now) {
			if err != nil || placed.Root != work {
				_ = work.Close()
			}

			return placed, err
		}

		if err == nil && placed.Root != work {
			_ = placed.Root.Close()
		}

		seen = now
	}

	_ = work.Close()

	return nil, errors.Errorf(
		"failed to place %q: the game server directories on its way kept changing, %d attempts made",
		rel, maxPlacements,
	)
}

// placer walks a request path down from the work directory and moves into
// every boundary the path passes.
type placer struct {
	bounds bounds
	// seen is what the names of the boundaries led to before the walk began.
	seen landmarks

	// work is the root the walk starts in. It is left open whatever happens.
	work *os.Root
	// root is the innermost boundary reached, opened at scope.
	root  *os.Root
	scope string
	// at is where the walk stands inside root. No link is left in it.
	at string

	inRoot   bool
	inServer bool
	// nested is set while the daemon knows a game server directory by a name
	// below the boundary the walk stands in.
	nested bool
	hops   int
}

// place walks rel down from work, telling the boundaries by seen. The root of
// what it returns is work or one it opened; nothing else is left open.
func (b bounds) place(work *os.Root, seen landmarks, rel string, target Target) (*Confined, error) {
	p := &placer{bounds: b, seen: seen, work: work, root: work, scope: ".", at: ".", nested: b.holds(".")}

	var names []string
	if rel != "." {
		names = strings.Split(rel, "/")
	}

	for i, name := range names {
		moved, err := p.step(name, i == len(names)-1, target)
		if err != nil {
			p.leave()

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
		p.leave()

		return nil, ErrPathOutsideConfinement
	}

	return &Confined{Root: p.root, Rel: p.at, Scope: p.scope, Innermost: p.inServer && !p.nested}, nil
}

// leave closes the boundary the walk stands in.
func (p *placer) leave() {
	if p.root != p.work {
		_ = p.root.Close()
	}
}

// step takes the walk one component of the request path further and reports
// whether it got there. Where it did not, the rest of the path is left the way
// the request spells it, for os.Root to resolve from where the walk stands.
func (p *placer) step(name string, last bool, target Target) (bool, error) {
	// No boundary is left to be found.
	if !p.seen.hasServers() && (p.seen.root == nil || p.inRoot) {
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
	if p.seen.root == nil || p.inRoot {
		return false, nil
	}

	info, err := p.root.Stat(path.Join(p.at, name))
	if err != nil || !os.SameFile(info, p.seen.root) {
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
	// One directory can be known by several names.
	var known []string

	for i, dir := range p.seen.servers {
		if dir != nil && os.SameFile(info, dir) {
			known = append(known, p.bounds.servers[i])
		}
	}

	isRoot := p.seen.root != nil && os.SameFile(info, p.seen.root)
	isServer := len(known) > 0

	if !isRoot && !isServer {
		return nil
	}

	if isRoot {
		known = append(known, p.bounds.root)
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

	p.leave()

	p.root, p.scope, p.at = inner, dir, "."
	p.inRoot = p.inRoot || isRoot
	p.inServer = p.inServer || isServer
	p.nested = slices.ContainsFunc(known, p.bounds.holds)

	return nil
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
