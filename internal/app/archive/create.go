package archive

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"

	"github.com/pkg/errors"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
)

// sourceEntry is one item scheduled for archiving: a regular file, a
// directory or a symlink stored as-is.
type sourceEntry struct {
	rel  string // path inside the root
	name string // entry name inside the archive, relative to base_path
	info os.FileInfo
	link string // symlink target, set when the symlink is stored as a symlink
}

func (e sourceEntry) isSymlink() bool {
	return e.info.Mode()&os.ModeSymlink != 0
}

// Create packs the requested sources into archive_path. See the package doc
// for the confinement model.
func Create(ctx context.Context, workDir string, p *pb.CreateArchiveParams, progress ProgressFunc) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "create archive canceled")
	}

	if len(p.GetSources()) == 0 {
		return nil, errors.New("no sources given")
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

	// The proto resolves an unset create format from the target file extension.
	format := p.GetFormat()
	if format == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		if format = formatFromExtension(archiveRel); format == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
			return nil, errors.Errorf(
				"archive format is unspecified and %q has no known extension", p.GetArchivePath(),
			)
		}
	}

	class, err := classifyForCreate(format)
	if err != nil {
		return nil, err
	}

	baseRel, err := fsutil.RootRel(p.GetBasePath())
	if err != nil {
		return nil, err
	}

	if parent := path.Dir(archiveRel); parent != "." && parent != "/" {
		if err := root.MkdirAll(parent, 0o755); err != nil {
			return nil, errors.Wrapf(err, "failed to create directory %q", parent)
		}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if p.GetOverwrite() {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}

	archiveFile, err := root.OpenFile(archiveRel, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errors.Errorf("archive %q already exists and overwrite is disabled", p.GetArchivePath())
		}

		return nil, errors.Wrap(err, "failed to create archive file")
	}

	acc := newAccumulator(p.GetMaxTotalBytes(), p.GetMaxFiles(), progress)

	createErr := createInto(ctx, root, archiveFile, baseRel, p, format, class, acc)

	closeErr := archiveFile.Close()
	if createErr != nil {
		// A failed operation must not leave a partial archive behind.
		_ = root.Remove(archiveRel)

		return nil, createErr
	}
	if closeErr != nil {
		_ = root.Remove(archiveRel)

		return nil, errors.Wrap(closeErr, "failed to close archive file")
	}

	// Everything past this point still counts as a failed operation, so the
	// archive is removed rather than left behind with the wrong mode or owner.
	if p.GetMode() != 0 {
		if err := root.Chmod(archiveRel, os.FileMode(p.GetMode()).Perm()); err != nil {
			_ = root.Remove(archiveRel)

			return nil, errors.Wrap(err, "failed to chmod archive file")
		}
	}

	owner := ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid())
	if err := osowner.ApplyToPathInRoot(root, archiveRel, owner); err != nil {
		_ = root.Remove(archiveRel)

		return nil, errors.Wrap(err, "failed to apply archive owner")
	}

	info, err := root.Stat(archiveRel)
	if err != nil {
		_ = root.Remove(archiveRel)

		return nil, errors.Wrap(err, "failed to stat archive file")
	}

	return &Result{
		FilesProcessed: acc.files,
		BytesProcessed: acc.bytes,
		ArchiveSize:    info.Size(),
		Format:         format,
	}, nil
}

func createInto(
	ctx context.Context,
	root *os.Root,
	archiveFile *os.File,
	baseRel string,
	p *pb.CreateArchiveParams,
	format pb.ArchiveFormat,
	class formatClass,
	acc *accumulator,
) error {
	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat archive file")
	}

	entries, err := collectSources(ctx, root, baseRel, p.GetSources(), &walkLimits{
		follow:     p.GetFollowSymlinks(),
		maxEntries: acc.maxFiles,
		archive:    archiveInfo,
	})
	if err != nil {
		return err
	}

	if class == classSingle {
		return createSingle(root, archiveFile, entries, p, format, acc)
	}

	if len(entries) == 0 {
		return errors.New("nothing to archive: sources contain no files")
	}

	if class == classZip {
		return createZip(ctx, root, archiveFile, entries, p, acc)
	}

	return createTar(ctx, root, archiveFile, entries, p, tarCompression(format), acc)
}

// walkLimits bounds one source expansion.
type walkLimits struct {
	follow bool
	// maxEntries stops the expansion itself, not just the write phase. The
	// whole entry list is materialized before a single byte is archived, and
	// with follow_symlinks a handful of links fans out into an enormous number
	// of distinct paths, so the limit has to apply here too.
	maxEntries uint64
	// archive identifies the file being written, so it is never archived into
	// itself. Comparing identity rather than the path name also covers reaching
	// it through a symlink, where the path differs.
	archive os.FileInfo
}

// sourceWalker expands the request sources (relative to base_path) into a flat
// entry list. Directories are walked recursively; symlinks are stored as
// symlinks unless follow is set, in which case the symlink target contents are
// archived (os.Root still refuses targets outside the work directory).
type sourceWalker struct {
	ctx     context.Context
	root    *os.Root
	limits  *walkLimits
	entries []sourceEntry
}

// collectSources expands every source into one flat, bounded entry list.
func collectSources(
	ctx context.Context, root *os.Root, baseRel string, sources []string, limits *walkLimits,
) ([]sourceEntry, error) {
	w := &sourceWalker{ctx: ctx, root: root, limits: limits}

	for _, src := range sources {
		srcRel, err := fsutil.RootRel(src)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid source %q", src)
		}

		rel := srcRel
		if baseRel != "." {
			rel = path.Join(baseRel, srcRel)
		}

		if err := w.walk(rel, srcRel, 0); err != nil {
			return nil, err
		}
	}

	return w.entries, nil
}

func (w *sourceWalker) add(e sourceEntry) error {
	if uint64(len(w.entries)) >= w.limits.maxEntries {
		return errors.Errorf("max files limit exceeded (%d)", w.limits.maxEntries)
	}

	w.entries = append(w.entries, e)

	return nil
}

// copySource streams one source file into an archive entry writer, capping
// the read one byte past the remaining byte budget so an oversized payload is
// reported by the accumulator instead of silently truncated.
func copySource(root *os.Root, rel string, w io.Writer, bytesLeft int64) (int64, error) {
	src, err := root.Open(rel)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to open source %q", rel)
	}
	defer src.Close()

	n, err := io.Copy(w, io.LimitReader(src, bytesLeft+1))
	if err != nil {
		return 0, errors.Wrapf(err, "failed to write %q", rel)
	}

	return n, nil
}

func (w *sourceWalker) walk(rel, name string, symlinkDepth int) error {
	if err := w.ctx.Err(); err != nil {
		return errors.Wrap(err, "create archive canceled")
	}

	info, err := w.root.Lstat(rel)
	if err != nil {
		return errors.Wrapf(err, "failed to stat source %q", rel)
	}

	if os.SameFile(info, w.limits.archive) {
		return nil
	}

	if info.Mode()&os.ModeSymlink != 0 && w.limits.follow {
		symlinkDepth++
		if symlinkDepth > maxFollowDepth {
			return errors.Errorf("symlink nesting too deep at %q", rel)
		}

		info, err = w.root.Stat(rel)
		if err != nil {
			return errors.Wrapf(err, "failed to resolve symlink %q", rel)
		}

		// Following the link is what makes it point at a file: a link aimed at
		// the archive only becomes the archive here, after the resolution.
		if os.SameFile(info, w.limits.archive) {
			return nil
		}
	}

	switch {
	case info.IsDir():
		return w.walkDir(rel, name, info, symlinkDepth)
	case info.Mode()&os.ModeSymlink != 0:
		link, err := w.root.Readlink(rel)
		if err != nil {
			return errors.Wrapf(err, "failed to read symlink %q", rel)
		}

		return w.add(sourceEntry{rel: rel, name: name, info: info, link: link})
	case info.Mode().IsRegular():
		return w.add(sourceEntry{rel: rel, name: name, info: info})
	default:
		// Sockets, fifos and device nodes cannot be archived; game-server work
		// directories legitimately contain unix sockets. Matches fsutil.Copy.
		return nil
	}
}

func (w *sourceWalker) walkDir(rel, name string, info os.FileInfo, symlinkDepth int) error {
	// The "." source contributes its children only; storing "." itself would
	// produce a useless root entry.
	if name != "." {
		if err := w.add(sourceEntry{rel: rel, name: name, info: info}); err != nil {
			return err
		}
	}

	dirEntries, err := fs.ReadDir(w.root.FS(), rel)
	if err != nil {
		return errors.Wrapf(err, "failed to read directory %q", rel)
	}

	for _, child := range dirEntries {
		childName := child.Name()
		if name != "." {
			childName = path.Join(name, child.Name())
		}

		if err := w.walk(path.Join(rel, child.Name()), childName, symlinkDepth); err != nil {
			return err
		}
	}

	return nil
}
