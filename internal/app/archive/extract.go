package archive

import (
	"cmp"
	"context"
	"io"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/pkg/errors"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
)

const (
	defaultFilePerm os.FileMode = 0o644
	defaultDirPerm  os.FileMode = 0o755

	// ownerDirAccess is what the owner of a directory needs to put entries
	// into it.
	ownerDirAccess os.FileMode = 0o700
)

// sink places extracted entries under the destination inside the root,
// applying the conflict policy, permission rules, ownership and limits.
type sink struct {
	root   *os.Root
	dest   string
	policy pb.ArchiveConflictPolicy
	// preserveFiles and preserveDirs take the permission bits of file and of
	// directory entries from the archive.
	preserveFiles bool
	preserveDirs  bool
	mode          os.FileMode // when != 0 overrides file permissions from the archive
	owner         osowner.Options
	acc           *accumulator
	skipped       []string
	links         []createdLink

	// mergeLinkedDirs lets a directory entry that meets a symlink go on through
	// it, as long as the root follows the link to a directory. Without it the
	// link is an entry in the way like any other and falls to the policy.
	mergeLinkedDirs bool
	// skipUnsafeLinks leaves out a symlink entry whose target is not confined
	// to the destination instead of failing the run, and records it.
	skipUnsafeLinks bool
	unsafeLinks     []SkippedLink
	// keepDirs fails the run where an entry that is not a directory meets a
	// directory, instead of taking the directory and all below it away.
	keepDirs bool
	// applyDestMode gives the destination the mode of the entry the archive has
	// for it, "./". Without it that entry is passed over.
	applyDestMode bool
	// dirModes holds the directories whose owner the archive keeps from writing
	// into them, and the mode each of them is to end up with. They are noted
	// by their physical path: an archive can name a directory in more than one
	// way. See setDirMode and physical.
	dirModes map[string]os.FileMode
	// undoCreated notes in created, in the order they come, the entries the
	// run puts where nothing stood, for removeCreated to take away again. They
	// are noted by their physical path as well.
	undoCreated bool
	created     []string
	// entryDir is the directory physicalEntry was asked about last, and what
	// it resolved to: the entries of an archive come directory by directory.
	entryDir entryDir
}

type entryDir struct {
	known    bool
	name     string
	physical string
}

// createdLink records a symlink this run put on disk so its target can be
// checked again once the archive can no longer move anything.
type createdLink struct {
	path   string // where the link was stored, relative to the root
	target string // literal target as it came out of the archive
}

// safeEntryName validates an archive entry name and returns its path relative
// to the root (dest-prefixed). ok=false means the entry is a "." artifact and
// should be skipped silently. Absolute names and names escaping the
// destination through ".." are rejected (zip-slip); os.Root remains the hard
// boundary against symlink escapes below.
func (s *sink) safeEntryName(name string) (target string, ok bool, err error) {
	// Windows-produced archives may use backslashes as separators; normalize
	// so ".." segments hidden behind them are caught too.
	clean := path.Clean(strings.ReplaceAll(name, `\`, "/"))
	if clean == "." {
		return "", false, nil
	}

	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, errors.Errorf("archive entry %q escapes the destination", name)
	}

	return path.Join(s.dest, clean), true, nil
}

func (s *sink) resolveConflict(target string, isDir bool) (skip, existed bool, err error) {
	existing, lstatErr := s.root.Lstat(target)
	if lstatErr != nil {
		if errors.Is(lstatErr, os.ErrNotExist) {
			return false, false, nil
		}

		if s.mergeLinkedDirs {
			if linkErr := s.linkOnTheWay(path.Dir(target)); linkErr != nil {
				return false, false, linkErr
			}
		}

		return false, false, errors.Wrapf(lstatErr, "failed to stat %q", target)
	}

	// An existing directory merging with a directory entry is not a conflict.
	if isDir && existing.IsDir() {
		return false, true, nil
	}

	if isDir && s.mergeLinkedDirs && existing.Mode()&os.ModeSymlink != 0 {
		return false, true, s.linkedDir(target)
	}

	switch s.policy {
	case pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_SKIP:
		return true, true, nil
	case pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE:
		// What makes room may be a link the directory resolved last was
		// reached through.
		s.entryDir = entryDir{}

		if existing.IsDir() {
			if s.keepDirs {
				return false, true, errors.Errorf(
					"%q is a directory, and the archive has an entry under that name that is not one", target,
				)
			}

			removed := s.physical(target)

			if err := s.root.RemoveAll(target); err != nil {
				return false, true, errors.Wrapf(err, "failed to remove %q", target)
			}

			// The directories below went with it, and so do the modes noted
			// for them. Where the removal fails the notes stay: what is left
			// of the directories still gets its modes.
			s.forgetDirModes(removed)

			return false, false, nil
		}

		if err := s.root.Remove(target); err != nil {
			return false, true, errors.Wrapf(err, "failed to remove %q", target)
		}

		return false, false, nil
	default: // UNSPECIFIED and ERROR both fail fast, per the proto contract
		return false, true, errors.Errorf("destination entry %q already exists", target)
	}
}

// linkedDir reports why the symlink standing at target cannot take the place
// of a directory: the root does not follow it, or it leads to something else.
func (s *sink) linkedDir(target string) error {
	info, err := s.root.Stat(target)
	if err != nil {
		return errors.Wrapf(err, "%q is a symlink that is not followed inside the destination", target)
	}

	if !info.IsDir() {
		return errors.Errorf("%q is a symlink that does not lead to a directory", target)
	}

	return nil
}

// linkOnTheWay names the first symlink on the way to dir that cannot take the
// place of a directory. The root refuses such a path as a whole, which does not
// say where on it the link stands.
func (s *sink) linkOnTheWay(dir string) error {
	passed := "."

	for _, name := range strings.Split(dir, "/") {
		passed = path.Join(passed, name)

		info, err := s.root.Lstat(passed)
		if err != nil {
			return nil
		}

		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		if err := s.linkedDir(passed); err != nil {
			return err
		}
	}

	return nil
}

func (s *sink) filePerm(archiveMode os.FileMode) os.FileMode {
	if s.mode != 0 {
		return s.mode
	}
	if s.preserveFiles && archiveMode.Perm() != 0 {
		return archiveMode.Perm()
	}

	return defaultFilePerm
}

func (s *sink) dirPerm(archiveMode os.FileMode) os.FileMode {
	if s.preserveDirs && archiveMode.Perm() != 0 {
		return archiveMode.Perm()
	}

	return defaultDirPerm
}

func (s *sink) putDir(name string, archiveMode os.FileMode) error {
	target, ok, err := s.safeEntryName(name)
	if err != nil {
		return err
	}
	if !ok {
		// An entry without permission bits records none, here as in dirPerm.
		if s.applyDestMode && s.preserveDirs && archiveMode.Perm() != 0 {
			return s.setDirMode(s.dest, archiveMode.Perm())
		}

		return nil
	}

	skip, existed, err := s.resolveConflict(target, true)
	if err != nil {
		return err
	}
	if skip {
		s.skipped = append(s.skipped, name)

		return nil
	}

	perm := s.dirPerm(archiveMode)

	// The directories on the way are only implied by this entry and get the
	// default mode, not the one the archive has for the directory itself.
	if err := s.ensureParent(target); err != nil {
		return err
	}

	if err := s.mkdirAll(target, perm|ownerDirAccess); err != nil {
		return err
	}

	// A directory that was there keeps its mode unless the permissions of the
	// archive are asked for and the entry has some: the default is for a
	// directory that is created.
	if !existed || (s.preserveDirs && archiveMode.Perm() != 0) {
		if err := s.setDirMode(target, perm); err != nil {
			return err
		}
	}

	if err := osowner.ApplyToPathInRoot(s.root, target, s.owner); err != nil {
		return errors.Wrapf(err, "failed to apply owner to %q", target)
	}

	return s.acc.addEntry(name, 0)
}

// setDirMode gives the directory at target the mode perm.
//
// A mode that keeps the owner of the directory from writing into it is not
// applied right away. The entries of the directory come after its own, and a
// daemon that is not root could no longer create them: the directory stays
// open to its owner, and to nobody the mode does not name, until applyDirModes
// settles it.
//
// The mode an archive has for a directory last is the one it ends up with,
// under whichever of its names the entries come.
func (s *sink) setDirMode(target string, perm os.FileMode) error {
	held := perm&ownerDirAccess != ownerDirAccess

	// The path is resolved only where there is a note to take or to drop: the
	// directories of an archive that closes none of them cost nothing more.
	var dir string

	if held || len(s.dirModes) > 0 {
		dir = s.physical(target)
		delete(s.dirModes, dir)
	}

	if err := s.root.Chmod(target, perm|ownerDirAccess); err != nil {
		return errors.Wrapf(err, "failed to chmod directory %q", target)
	}

	if !held {
		return nil
	}

	if s.dirModes == nil {
		s.dirModes = make(map[string]os.FileMode)
	}

	s.dirModes[dir] = perm

	return nil
}

// physical returns the path of what target names with every symlink on the
// way to it, and the one it may end in, resolved.
//
// The notes the sink keeps for later go by it. An entry reaches a directory
// through whatever links stand in the tree, and a later entry may take such a
// link away or point it elsewhere: the name the first entry came under then
// leads to nothing, or to another directory. The physical path goes on naming
// the same one. It is also one for all the names of a directory, which is how
// two entries about the same directory are found to be.
//
// A path that does not resolve is returned as it is: what it leads to is for
// the root to say.
func (s *sink) physical(target string) string {
	resolved, err := s.resolveLinkTarget(".", target)
	if err != nil {
		return target
	}

	return resolved
}

// physicalEntry is physical for an entry that is not followed itself: a file,
// or a symlink the archive stores.
func (s *sink) physicalEntry(target string) string {
	dir := path.Dir(target)

	if !s.entryDir.known || s.entryDir.name != dir {
		s.entryDir = entryDir{known: true, name: dir, physical: s.physical(dir)}
	}

	return path.Join(s.entryDir.physical, path.Base(target))
}

// below reports whether name is dir or lies inside it. Both are paths relative
// to the root, and "." holds them all.
func below(dir, name string) bool {
	return dir == "." || name == dir || strings.HasPrefix(name, dir+"/")
}

// applyDirModes gives the directories the modes setDirMode held back, the
// deepest first: a directory is reached through the ones above it, and one of
// those may be about to become impassable.
//
// It goes through all of them whatever becomes of one, and returns the first
// error.
func (s *sink) applyDirModes() error {
	dirs := slices.SortedFunc(maps.Keys(s.dirModes), func(a, b string) int {
		return cmp.Or(cmp.Compare(dirDepth(b), dirDepth(a)), cmp.Compare(a, b))
	})

	var firstErr error

	for _, dir := range dirs {
		// What stands there by now need not be the directory the mode was noted
		// for: a run that failed has taken away what it created, and the owner
		// of the tree may have put something else in its place.
		info, err := s.root.Lstat(dir)
		if err != nil || !info.IsDir() {
			continue
		}

		if err := s.root.Chmod(dir, s.dirModes[dir]); err != nil && firstErr == nil {
			firstErr = errors.Wrapf(err, "failed to chmod directory %q", dir)
		}
	}

	s.dirModes = nil

	return firstErr
}

// forgetDirModes drops the modes held back for the directory with the physical
// path dir and for the ones below it, which have been taken away.
func (s *sink) forgetDirModes(dir string) {
	for held := range s.dirModes {
		if below(dir, held) {
			delete(s.dirModes, held)
		}
	}
}

// dirDepth is the number of directories on the way from the root to target,
// target included.
func dirDepth(target string) int {
	if target == "." {
		return 0
	}

	return strings.Count(target, "/") + 1
}

func (s *sink) putFile(name string, archiveMode os.FileMode, r io.Reader) error {
	target, ok, err := s.safeEntryName(name)
	if err != nil {
		return err
	}
	if !ok {
		// A "." entry still has to be consumed by sequential readers; the
		// caller passes the reader and drains it here.
		_, err := io.Copy(io.Discard, r)

		return errors.Wrap(err, "failed to skip archive entry")
	}

	fresh := s.undoCreated && s.absent(target)

	skip, _, err := s.resolveConflict(target, false)
	if err != nil {
		return err
	}
	if skip {
		s.skipped = append(s.skipped, name)
		_, err := io.Copy(io.Discard, r)

		return errors.Wrap(err, "failed to skip archive entry")
	}

	perm := s.filePerm(archiveMode)

	if err := s.ensureParent(target); err != nil {
		return err
	}

	out, err := s.root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return errors.Wrapf(err, "failed to create file %q", target)
	}

	if fresh {
		s.created = append(s.created, s.physicalEntry(target))
	}

	n, copyErr := io.Copy(out, io.LimitReader(r, s.acc.bytesLeft()+1))
	closeErr := out.Close()

	if copyErr != nil {
		_ = s.root.Remove(target)

		return errors.Wrapf(copyErr, "failed to write file %q", target)
	}
	if closeErr != nil {
		_ = s.root.Remove(target)

		return errors.Wrapf(closeErr, "failed to close file %q", target)
	}

	if err := s.acc.addEntry(name, n); err != nil {
		_ = s.root.Remove(target)

		return err
	}

	// OpenFile applies umask; chmod for the exact requested permissions.
	if err := s.root.Chmod(target, perm); err != nil {
		return errors.Wrapf(err, "failed to chmod file %q", target)
	}

	if err := osowner.ApplyToPathInRoot(s.root, target, s.owner); err != nil {
		return errors.Wrapf(err, "failed to apply owner to %q", target)
	}

	return nil
}

// maxSymlinkResolveHops bounds symlink expansion while validating a link
// target. It matches os.Root's own rootMaxSymlinks, so a target accepted here
// is one os.Root will also agree to resolve later.
const maxSymlinkResolveHops = 8

func (s *sink) withinDest(resolved string) bool {
	if s.dest == "." {
		return resolved != ".." && !strings.HasPrefix(resolved, "../")
	}

	return resolved == s.dest || strings.HasPrefix(resolved, s.dest+"/")
}

// resolveLinkTarget resolves linkTarget the way the kernel would: relative to
// the directory holding the link, expanding any path component that is itself
// a symlink already present under the root.
//
// Resolving lexically is not enough. An archive can first store dst/a/b/l with
// target "../..", which cleans to dst and is accepted, and then store dst/esc
// with target "a/b/l/../../../x". That cleans to dst/x — apparently inside the
// destination — while the kernel walks it to three levels above the work
// directory, leaving an escaping symlink on disk for whatever reads that tree
// without os.Root.
func (s *sink) resolveLinkTarget(linkDir, linkTarget string) (string, error) {
	cur := linkDir
	pending := strings.Split(linkTarget, "/")
	hops := 0

	for len(pending) > 0 {
		part := pending[0]
		pending = pending[1:]

		switch part {
		case "", ".":
			continue
		case "..":
			// Above the root there is nothing of the destination left: the
			// root is the directory the request is confined to.
			if cur == "." {
				return "", errors.New("symlink target escapes the destination")
			}
			cur = path.Dir(cur)

			continue
		}

		next := part
		if cur != "." {
			next = path.Join(cur, part)
		}

		info, statErr := s.root.Lstat(next)
		if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
			// Missing entries resolve literally: the archive may create them
			// later, and a dangling link is not by itself an escape.
			cur = next

			continue
		}

		hops++
		if hops > maxSymlinkResolveHops {
			return "", errors.New("symlink target crosses too many links")
		}

		nested, linkErr := s.root.Readlink(next)
		if linkErr != nil {
			return "", errors.Wrapf(linkErr, "failed to read symlink %q", next)
		}
		if path.IsAbs(nested) {
			return "", errors.New("symlink target crosses an absolute symlink")
		}

		// The kernel replaces the link with its target resolved from the
		// directory holding it, so cur stays put and the target is spliced in
		// front of what is left to walk.
		pending = append(strings.Split(nested, "/"), pending...)
	}

	return cur, nil
}

// checkLinkTarget reports whether the symlink stored at linkPath with the given
// literal target stays inside the destination.
//
// The directory holding the link is resolved first: os.Root creates the link in
// the directory linkPath resolves to, so a parent component that is itself a
// symlink moves the link — and with it what every ".." in its target pops off.
// An archive storing "s -> ." and then "s/l -> ../x" has os.Root put l next to
// s instead of below it, which turns a target the lexical path says is inside
// the destination into one that leaves it.
func (s *sink) checkLinkTarget(linkPath, linkTarget string) error {
	if path.IsAbs(linkTarget) {
		return errors.New("absolute symlink target")
	}

	linkDir, err := s.resolveLinkTarget(".", path.Dir(linkPath))
	if err != nil {
		return err
	}

	resolved, err := s.resolveLinkTarget(linkDir, linkTarget)
	if err != nil {
		return err
	}
	if !s.withinDest(resolved) {
		return errors.New("symlink target escapes the destination")
	}

	return nil
}

// revalidateLinks checks every symlink this run created once more, against the
// finished tree.
//
// checkLinkTarget can only see the tree as it stands when the entry arrives. A
// later entry can turn a component an earlier target resolved through into a
// symlink of its own — either by being stored after it or by replacing a
// directory under the overwrite policy — so two links that are each confined on
// their own combine into one that is not.
//
// A link that fails invalidates the whole run, so every link it created is
// removed: the checks are order dependent, and dropping only the offenders
// would leave the links validated before them resting on a tree that changed
// underneath.
func (s *sink) revalidateLinks() error {
	for _, l := range s.links {
		// A later entry may have taken the link away again (an overwritten
		// directory takes its whole subtree with it); whatever sits there now
		// belongs to that entry, not to this one.
		info, err := s.root.Lstat(l.path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		if err := s.checkLinkTarget(l.path, l.target); err != nil {
			s.removeCreatedLinks()

			return errors.Wrapf(err, "symlink %q", l.path)
		}
	}

	return nil
}

func (s *sink) removeCreatedLinks() {
	for _, l := range s.links {
		if info, err := s.root.Lstat(l.path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}

		_ = s.root.Remove(l.path)
	}
}

func (s *sink) putSymlink(name, linkTarget string) error {
	target, ok, err := s.safeEntryName(name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	if err := s.checkLinkTarget(target, linkTarget); err != nil {
		if s.skipUnsafeLinks {
			s.unsafeLinks = append(s.unsafeLinks, SkippedLink{Name: name, Target: linkTarget, Reason: err.Error()})

			return nil
		}

		return errors.Wrapf(err, "archive entry %q", name)
	}

	fresh := s.undoCreated && s.absent(target)

	skip, _, err := s.resolveConflict(target, false)
	if err != nil {
		return err
	}
	if skip {
		s.skipped = append(s.skipped, name)

		return nil
	}

	if err := s.ensureParent(target); err != nil {
		return err
	}

	if err := s.root.Symlink(linkTarget, target); err != nil {
		return errors.Wrapf(err, "failed to create symlink %q", target)
	}

	s.links = append(s.links, createdLink{path: target, target: linkTarget})

	if fresh {
		s.created = append(s.created, s.physicalEntry(target))
	}

	if err := osowner.ApplyToPathInRoot(s.root, target, s.owner); err != nil {
		return errors.Wrapf(err, "failed to apply owner to %q", target)
	}

	return s.acc.addEntry(name, 0)
}

func (s *sink) ensureParent(target string) error {
	parent := path.Dir(target)
	if parent == "." || parent == "/" {
		return nil
	}

	return s.mkdirAll(parent, defaultDirPerm)
}

func (s *sink) mkdirAll(dir string, perm os.FileMode) error {
	created, err := mkdirAllOwned(s.root, dir, perm, s.owner)
	if s.undoCreated {
		for _, segment := range created {
			s.created = append(s.created, s.physical(segment))
		}
	}

	if err == nil || !s.mergeLinkedDirs {
		return err
	}

	if linkErr := s.linkOnTheWay(dir); linkErr != nil {
		return linkErr
	}

	return err
}

// mkdirAllOwned creates rel and applies perm and the owner to exactly the
// directories it had to create, leaving pre-existing (often shared) parents
// alone. Without the owner, a game server running as an unprivileged su_user
// cannot traverse into the tree the daemon just unpacked for it. The mode is
// set once more because creating a directory is subject to the umask of the
// daemon, which would otherwise decide who gets into a directory an entry
// only implies.
//
// It returns the directories that were not there before, the shallowest first,
// also when it fails: some of them may have been created by then.
func mkdirAllOwned(root *os.Root, rel string, perm os.FileMode, owner osowner.Options) ([]string, error) {
	created, err := osowner.MissingSegmentsInRoot(root, rel)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to inspect directory %q", rel)
	}

	if err := root.MkdirAll(rel, perm); err != nil {
		return created, errors.Wrapf(err, "failed to create directory %q", rel)
	}

	for _, segment := range created {
		if err := root.Chmod(segment, perm); err != nil {
			return created, errors.Wrapf(err, "failed to chmod directory %q", segment)
		}

		if err := osowner.ApplyToPathInRoot(root, segment, owner); err != nil {
			return created, errors.Wrapf(err, "failed to apply owner to %q", segment)
		}
	}

	return created, nil
}

// absent reports whether nothing stands at target.
func (s *sink) absent(target string) bool {
	_, err := s.root.Lstat(target)

	return errors.Is(err, os.ErrNotExist)
}

// removeCreated takes away what the run put where nothing stood before, the
// last entry first: a directory is empty by the time its turn comes. What does
// not go stays, such as a directory somebody else has written into meanwhile.
//
// The entries are taken away where they were created. The name one of them
// came under may lead elsewhere by now, to what the run never touched: a later
// entry may have replaced a link on the way.
//
// An entry the run replaced is not among them. What stood under its name is
// gone, and taking the entry away as well would leave nothing there at all.
func (s *sink) removeCreated() {
	for _, target := range slices.Backward(s.created) {
		_ = s.root.Remove(target)
	}

	s.created = nil
}

// Extract unpacks archive_path into destination. See the package doc for the
// confinement model and safeEntryName for the zip-slip rules.
func Extract(
	ctx context.Context,
	workDir string,
	p *pb.ExtractArchiveParams,
	progress ProgressFunc,
	confineOpts ...fsutil.ConfineOption,
) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "extract archive canceled")
	}

	confiner := fsutil.NewConfiner(workDir, confineOpts...)

	// The archive file and the tree it is unpacked into are placed separately:
	// they need not belong to the same game server directory.
	archive, err := confiner.Resolve(p.GetArchivePath(), fsutil.TargetEntry)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	archiveRel := archive.Rel

	dest, err := confiner.Resolve(p.GetDestination(), fsutil.TargetTree)
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	root, destRel := dest.Root, dest.Rel

	if err := prepareDestination(root, destRel, p); err != nil {
		return nil, err
	}

	archiveFile, err := archive.Root.Open(archiveRel)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open archive %q", p.GetArchivePath())
	}
	defer archiveFile.Close()

	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat archive %q", p.GetArchivePath())
	}

	// The proto lets the request leave the format unset and expects the daemon
	// to work it out from the content or the file name.
	format := p.GetFormat()
	if format == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		if format, err = detectFormat(archiveFile, archiveRel); err != nil {
			return nil, err
		}
	}

	class, err := classify(format)
	if err != nil {
		return nil, err
	}

	s := &sink{
		root:          root,
		dest:          destRel,
		policy:        p.GetConflictPolicy(),
		preserveFiles: p.GetPreservePermissions(),
		preserveDirs:  p.GetPreservePermissions(),
		mode:          os.FileMode(p.GetMode()).Perm(),
		owner:         ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid()),
		acc:           newAccumulator(p.GetMaxTotalBytes(), p.GetMaxFiles(), progress),
	}

	extractErr := extractEntries(ctx, archiveFile, archiveRel, class, format, s)

	// Symlink confinement is only settled once the archive can no longer move
	// anything, so it is decided here — including for a run that failed, which
	// leaves its partial tree behind and must not leave an escaping link in it.
	linkErr := s.revalidateLinks()

	// After the links: taking one away needs a directory that can be written
	// to. A run that failed settles the modes as well, so that no directory is
	// left more open to its owner than the archive has it.
	modeErr := s.applyDirModes()

	if extractErr != nil {
		return nil, extractErr
	}
	if linkErr != nil {
		return nil, linkErr
	}
	if modeErr != nil {
		return nil, modeErr
	}

	return &Result{
		FilesProcessed: s.acc.files,
		BytesProcessed: s.acc.bytes,
		// The proto defines archive_size as the source archive when extracting.
		ArchiveSize: archiveInfo.Size(),
		Skipped:     s.skipped,
		Format:      format,
	}, nil
}

func prepareDestination(root *os.Root, destRel string, p *pb.ExtractArchiveParams) error {
	info, err := root.Stat(destRel)
	switch {
	case err == nil:
		if !info.IsDir() {
			return errors.Errorf("destination %q is not a directory", p.GetDestination())
		}

		return nil
	case errors.Is(err, os.ErrNotExist):
		if !p.GetCreateDestination() {
			return errors.Errorf(
				"destination %q does not exist and create_destination is disabled", p.GetDestination(),
			)
		}

		owner := ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid())
		if _, err := mkdirAllOwned(root, destRel, defaultDirPerm, owner); err != nil {
			return errors.Wrapf(err, "failed to create destination %q", p.GetDestination())
		}

		return nil
	default:
		return errors.Wrapf(err, "failed to stat destination %q", p.GetDestination())
	}
}
