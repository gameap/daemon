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

	if err := s.root.MkdirAll(target, s.dirPerm(archiveMode)); err != nil {
		return errors.Wrapf(err, "failed to create directory %q", target)
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

func (s *sink) putSymlink(name, linkTarget string) error {
	target, ok, err := s.safeEntryName(name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	// The link itself must stay inside the destination: resolve the target
	// lexically relative to the directory containing the link and reject
	// absolute or escaping targets.
	if path.IsAbs(linkTarget) {
		return errors.Errorf("archive entry %q has an absolute symlink target", name)
	}

	resolved := path.Clean(path.Join(path.Dir(target), linkTarget))
	if s.dest == "." {
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return errors.Errorf("archive entry %q has a symlink target escaping the destination", name)
		}
	} else if resolved != s.dest && !strings.HasPrefix(resolved, s.dest+"/") {
		return errors.Errorf("archive entry %q has a symlink target escaping the destination", name)
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

	if err := s.root.MkdirAll(parent, defaultDirPerm); err != nil {
		return errors.Wrapf(err, "failed to create directory %q", parent)
	}

	return nil
}

// Extract unpacks archive_path into destination. See the package doc for the
// confinement model and safeEntryName for the zip-slip rules.
func Extract(ctx context.Context, workDir string, p *pb.ExtractArchiveParams, progress ProgressFunc) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "extract archive canceled")
	}

	if p.GetFormat() == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		return nil, errors.New("archive format is unspecified")
	}

	class, err := classify(p.GetFormat())
	if err != nil {
		return nil, err
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

	s := &sink{
		root:     root,
		dest:     destRel,
		policy:   p.GetConflictPolicy(),
		preserve: p.GetPreservePermissions(),
		mode:     os.FileMode(p.GetMode()).Perm(),
		owner:    ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid()),
		acc:      newAccumulator(p.GetMaxTotalBytes(), p.GetMaxFiles(), progress),
	}

	if err := extractEntries(ctx, archiveFile, archiveRel, class, p.GetFormat(), s); err != nil {
		return nil, err
	}

	return &Result{
		FilesProcessed: s.acc.files,
		BytesProcessed: s.acc.bytes,
		Skipped:        s.skipped,
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

		if err := root.MkdirAll(destRel, defaultDirPerm); err != nil {
			return errors.Wrapf(err, "failed to create destination %q", p.GetDestination())
		}

		owner := ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid())
		if err := osowner.ApplyToPathInRoot(root, destRel, owner); err != nil {
			return errors.Wrap(err, "failed to apply destination owner")
		}

		return nil
	default:
		return errors.Wrapf(err, "failed to stat destination %q", p.GetDestination())
	}
}
