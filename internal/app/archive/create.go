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

	class, err := classifyForCreate(p.GetFormat())
	if err != nil {
		return nil, err
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

	createErr := createInto(ctx, root, archiveFile, archiveRel, baseRel, p, class, acc)

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

	if p.GetMode() != 0 {
		if err := root.Chmod(archiveRel, os.FileMode(p.GetMode()).Perm()); err != nil {
			return nil, errors.Wrap(err, "failed to chmod archive file")
		}
	}

	owner := ownerOptions(p.GetOwnerUser(), p.GetOwnerUid(), p.GetOwnerGid())
	if err := osowner.ApplyToPathInRoot(root, archiveRel, owner); err != nil {
		return nil, errors.Wrap(err, "failed to apply archive owner")
	}

	info, err := root.Stat(archiveRel)
	if err != nil {
		return nil, errors.Wrap(err, "failed to stat archive file")
	}

	return &Result{
		FilesProcessed: acc.files,
		BytesProcessed: acc.bytes,
		ArchiveSize:    info.Size(),
	}, nil
}

func createInto(
	ctx context.Context,
	root *os.Root,
	archiveFile *os.File,
	archiveRel, baseRel string,
	p *pb.CreateArchiveParams,
	class formatClass,
	acc *accumulator,
) error {
	entries, err := collectSources(root, baseRel, p.GetSources(), p.GetFollowSymlinks(), archiveRel)
	if err != nil {
		return err
	}

	if class == classSingle {
		return createSingle(root, archiveFile, entries, p, acc)
	}

	if len(entries) == 0 {
		return errors.New("nothing to archive: sources contain no files")
	}

	if class == classZip {
		return createZip(ctx, root, archiveFile, entries, p, acc)
	}

	return createTar(ctx, root, archiveFile, entries, p, tarCompression(p.GetFormat()), acc)
}

// collectSources expands the request sources (relative to base_path) into a
// flat entry list. Directories are walked recursively; symlinks are stored
// as symlinks unless follow_symlinks is set, in which case the symlink target
// contents are archived (os.Root still refuses targets outside the work
// directory). The archive file itself is skipped when it sits inside the
// walked tree.
func collectSources(
	root *os.Root, baseRel string, sources []string, follow bool, archiveRel string,
) ([]sourceEntry, error) {
	var entries []sourceEntry

	for _, src := range sources {
		srcRel, err := fsutil.RootRel(src)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid source %q", src)
		}

		rel := srcRel
		if baseRel != "." {
			rel = path.Join(baseRel, srcRel)
		}

		if err := walkSource(root, rel, srcRel, follow, 0, archiveRel, &entries); err != nil {
			return nil, err
		}
	}

	return entries, nil
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

func walkSource(
	root *os.Root,
	rel, name string,
	follow bool,
	depth int,
	archiveRel string,
	entries *[]sourceEntry,
) error {
	if depth > maxFollowDepth {
		return errors.Errorf("symlink nesting too deep at %q", rel)
	}

	info, err := root.Lstat(rel)
	if err != nil {
		return errors.Wrapf(err, "failed to stat source %q", rel)
	}

	if info.Mode()&os.ModeSymlink != 0 && follow {
		info, err = root.Stat(rel)
		if err != nil {
			return errors.Wrapf(err, "failed to resolve symlink %q", rel)
		}
	}

	switch {
	case info.IsDir():
		// The "." source contributes its children only; storing "." itself
		// would produce a useless root entry.
		if name != "." {
			*entries = append(*entries, sourceEntry{rel: rel, name: name, info: info})
		}

		dirEntries, err := fs.ReadDir(root.FS(), rel)
		if err != nil {
			return errors.Wrapf(err, "failed to read directory %q", rel)
		}

		for _, child := range dirEntries {
			childRel := path.Join(rel, child.Name())
			if childRel == archiveRel {
				continue
			}

			childName := child.Name()
			if name != "." {
				childName = path.Join(name, child.Name())
			}

			if err := walkSource(root, childRel, childName, follow, depth+1, archiveRel, entries); err != nil {
				return err
			}
		}

		return nil
	case info.Mode()&os.ModeSymlink != 0:
		link, err := root.Readlink(rel)
		if err != nil {
			return errors.Wrapf(err, "failed to read symlink %q", rel)
		}

		*entries = append(*entries, sourceEntry{rel: rel, name: name, info: info, link: link})

		return nil
	case info.Mode().IsRegular():
		*entries = append(*entries, sourceEntry{rel: rel, name: name, info: info})

		return nil
	default:
		// Sockets, fifos and device nodes cannot be archived; game-server work
		// directories legitimately contain unix sockets. Matches fsutil.Copy.
		return nil
	}
}
