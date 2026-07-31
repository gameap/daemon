package archive

import (
	"context"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/errors"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
)

const (
	defaultFilePerm os.FileMode = 0o644
	defaultDirPerm  os.FileMode = 0o755
)

// sink places extracted entries under the destination inside the root,
// applying the conflict policy, permission rules, ownership and limits.
type sink struct {
	root     *os.Root
	dest     string
	policy   pb.ArchiveConflictPolicy
	preserve bool
	mode     os.FileMode // when != 0 overrides file permissions from the archive
	owner    osowner.Options
	acc      *accumulator
	skipped  []string
	links    []createdLink
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

		return false, false, errors.Wrapf(lstatErr, "failed to stat %q", target)
	}

	// An existing directory merging with a directory entry is not a conflict.
	if isDir && existing.IsDir() {
		return false, true, nil
	}

	switch s.policy {
	case pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_SKIP:
		return true, true, nil
	case pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE:
		if existing.IsDir() {
			if err := s.root.RemoveAll(target); err != nil {
				return false, true, errors.Wrapf(err, "failed to remove %q", target)
			}
		} else if err := s.root.Remove(target); err != nil {
			return false, true, errors.Wrapf(err, "failed to remove %q", target)
		}

		return false, false, nil
	default: // UNSPECIFIED and ERROR both fail fast, per the proto contract
		return false, true, errors.Errorf("destination entry %q already exists", target)
	}
}

func (s *sink) filePerm(archiveMode os.FileMode) os.FileMode {
	if s.mode != 0 {
		return s.mode
	}
	if s.preserve && archiveMode.Perm() != 0 {
		return archiveMode.Perm()
	}

	return defaultFilePerm
}

func (s *sink) dirPerm(archiveMode os.FileMode) os.FileMode {
	if s.preserve && archiveMode.Perm() != 0 {
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

	if err := mkdirAllOwned(s.root, target, s.dirPerm(archiveMode), s.owner); err != nil {
		return err
	}

	// MkdirAll applies umask; chmod for the exact requested permissions. A
	// pre-existing directory merged into keeps its mode unless permissions
	// are explicitly preserved from the archive.
	if !existed || s.preserve {
		if err := s.root.Chmod(target, s.dirPerm(archiveMode)); err != nil {
			return errors.Wrapf(err, "failed to chmod directory %q", target)
		}
	}

	if err := osowner.ApplyToPathInRoot(s.root, target, s.owner); err != nil {
		return errors.Wrapf(err, "failed to apply owner to %q", target)
	}

	return s.acc.addEntry(name, 0)
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
			if cur == "." {
				return "", errors.New("symlink target escapes the work directory")
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
		return errors.Wrapf(err, "archive entry %q", name)
	}

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

	return mkdirAllOwned(s.root, parent, defaultDirPerm, s.owner)
}

// mkdirAllOwned creates rel and applies the owner to exactly the directories it
// had to create, leaving pre-existing (often shared) parents alone. Without
// this, a game server running as an unprivileged su_user cannot traverse into
// the tree the daemon just unpacked for it.
func mkdirAllOwned(root *os.Root, rel string, perm os.FileMode, owner osowner.Options) error {
	created, err := osowner.MissingSegmentsInRoot(root, rel)
	if err != nil {
		return errors.Wrapf(err, "failed to inspect directory %q", rel)
	}

	if err := root.MkdirAll(rel, perm); err != nil {
		return errors.Wrapf(err, "failed to create directory %q", rel)
	}

	for _, segment := range created {
		if err := osowner.ApplyToPathInRoot(root, segment, owner); err != nil {
			return errors.Wrapf(err, "failed to apply owner to %q", segment)
		}
	}

	return nil
}

// Extract unpacks archive_path into destination. See the package doc for the
// confinement model and safeEntryName for the zip-slip rules.
func Extract(ctx context.Context, workDir string, p *pb.ExtractArchiveParams, progress ProgressFunc) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "extract archive canceled")
	}

	root, err := os.OpenRoot(workDir)
	if err != nil {
		return nil, errors.Wrap(err, "work directory unavailable")
	}
	defer root.Close()

	archiveRel, err := fsutil.RootRel(p.GetArchivePath())
	if err != nil {
		return nil, err
	}

	destRel, err := fsutil.RootRel(p.GetDestination())
	if err != nil {
		return nil, err
	}

	if err := prepareDestination(root, destRel, p); err != nil {
		return nil, err
	}

	archiveFile, err := root.Open(archiveRel)
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
		root:     root,
		dest:     destRel,
		policy:   p.GetConflictPolicy(),
		preserve: p.GetPreservePermissions(),
		mode:     os.FileMode(p.GetMode()).Perm(),
		owner:    ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid()),
		acc:      newAccumulator(p.GetMaxTotalBytes(), p.GetMaxFiles(), progress),
	}

	extractErr := extractEntries(ctx, archiveFile, archiveRel, class, format, s)

	// Symlink confinement is only settled once the archive can no longer move
	// anything, so it is decided here — including for a run that failed, which
	// leaves its partial tree behind and must not leave an escaping link in it.
	linkErr := s.revalidateLinks()

	if extractErr != nil {
		return nil, extractErr
	}
	if linkErr != nil {
		return nil, linkErr
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
		if err := mkdirAllOwned(root, destRel, defaultDirPerm, owner); err != nil {
			return errors.Wrapf(err, "failed to create destination %q", p.GetDestination())
		}

		return nil
	default:
		return errors.Wrapf(err, "failed to stat destination %q", p.GetDestination())
	}
}
