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
	root *os.Root // the root the entry is read through
	rel  string   // path inside the root
	name string   // entry name inside the archive, relative to base_path
	info os.FileInfo
	link string // symlink target, set when the symlink is stored as a symlink
}

func (e sourceEntry) isSymlink() bool {
	return e.info.Mode()&os.ModeSymlink != 0
}

// Create packs the requested sources into archive_path. See the package doc
// for the confinement model.
func Create(
	ctx context.Context,
	workDir string,
	p *pb.CreateArchiveParams,
	progress ProgressFunc,
	confineOpts ...fsutil.ConfineOption,
) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "create archive canceled")
	}

	if len(p.GetSources()) == 0 {
		return nil, errors.New("no sources given")
	}

	confiner := fsutil.NewConfiner(workDir, confineOpts...)

	// The archive file and the tree it is packed from are placed separately:
	// they need not belong to the same game server directory.
	archive, err := confiner.Resolve(p.GetArchivePath(), fsutil.TargetEntry)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	root, archiveRel := archive.Root, archive.Rel

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

	base, err := confiner.Resolve(p.GetBasePath(), fsutil.TargetTree)
	if err != nil {
		return nil, err
	}
	defer base.Close()

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

	createErr := createInto(ctx, confiner, base, archiveFile, p, format, class, acc)

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
	confiner *fsutil.Confiner,
	base *fsutil.Confined,
	archiveFile *os.File,
	p *pb.CreateArchiveParams,
	format pb.ArchiveFormat,
	class formatClass,
	acc *accumulator,
) error {
	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat archive file")
	}

	walker := &sourceWalker{
		ctx:      ctx,
		confiner: confiner,
		limits: &walkLimits{
			follow:     p.GetFollowSymlinks(),
			maxEntries: acc.maxFiles,
			archive:    archiveInfo,
		},
	}
	defer walker.close()

	entries, err := walker.collect(base, p.GetBasePath(), p.GetSources())
	if err != nil {
		return err
	}

	if class == classSingle {
		return createSingle(archiveFile, entries, p, format, acc)
	}

	if len(entries) == 0 {
		return errors.New("nothing to archive: sources contain no files")
	}

	if class == classZip {
		return createZip(ctx, archiveFile, entries, p, acc)
	}

	return createTar(ctx, archiveFile, entries, p, tarCompression(format), acc)
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
// archived.
//
// Every source is placed by the confiner on its own, and a game server
// directory the walk enters from above is read through a root opened at it.
// A tree packed for the node is thus confined the way a tree packed for one
// game server is: os.Root refuses a link leading out of the game server
// directory it stands in, whichever directory the request started from.
type sourceWalker struct {
	ctx      context.Context
	confiner *fsutil.Confiner
	limits   *walkLimits
	entries  []sourceEntry
	scopes   []sourceScope
}

// sourceScope is a root the sources are read through.
type sourceScope struct {
	root *os.Root
	dir  os.FileInfo
	// owned roots are opened by the walker and closed with it.
	owned bool
}

// close releases the roots the entries are read through, so it has to wait
// until the archive is written.
func (w *sourceWalker) close() {
	for _, scope := range w.scopes {
		if scope.owned {
			_ = scope.root.Close()
		}
	}
}

// collect expands every source into one flat, bounded entry list.
func (w *sourceWalker) collect(base *fsutil.Confined, basePath string, sources []string) ([]sourceEntry, error) {
	baseRel, err := fsutil.RootRel(basePath)
	if err != nil {
		return nil, err
	}

	if _, err := w.adopt(base.Root, false); err != nil {
		return nil, err
	}

	for _, src := range sources {
		srcRel, err := fsutil.RootRel(src)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid source %q", src)
		}

		placed, err := w.confiner.Resolve(path.Join(baseRel, srcRel), fsutil.TargetTree)
		if err != nil {
			return nil, err
		}

		root, err := w.adopt(placed.Root, true)
		if err != nil {
			return nil, err
		}

		if err := w.walk(root, placed.Rel, srcRel, 0); err != nil {
			return nil, err
		}
	}

	return w.entries, nil
}

// adopt returns the root to read the directory of root through: the one
// already open there, or root itself, which is then kept. The sources of one
// request mostly share a directory, and a link can lead to a game server
// directory any number of times; neither may cost a descriptor each.
func (w *sourceWalker) adopt(root *os.Root, owned bool) (*os.Root, error) {
	dir, err := root.Stat(".")
	if err != nil {
		if owned {
			_ = root.Close()
		}

		return nil, errors.Wrap(err, "failed to stat source directory")
	}

	if known := w.scopeOf(dir); known != nil {
		if owned {
			_ = root.Close()
		}

		return known, nil
	}

	w.scopes = append(w.scopes, sourceScope{root: root, dir: dir, owned: owned})

	return root, nil
}

func (w *sourceWalker) scopeOf(dir os.FileInfo) *os.Root {
	for _, scope := range w.scopes {
		if os.SameFile(scope.dir, dir) {
			return scope.root
		}
	}

	return nil
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

func (w *sourceWalker) walk(root *os.Root, rel, name string, symlinkDepth int) error {
	if err := w.ctx.Err(); err != nil {
		return errors.Wrap(err, "create archive canceled")
	}

	info, err := root.Lstat(rel)
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

		info, err = root.Stat(rel)
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
		return w.walkDir(root, rel, name, info, symlinkDepth)
	case info.Mode()&os.ModeSymlink != 0:
		link, err := root.Readlink(rel)
		if err != nil {
			return errors.Wrapf(err, "failed to read symlink %q", rel)
		}

		return w.add(sourceEntry{root: root, rel: rel, name: name, info: info, link: link})
	case info.Mode().IsRegular():
		return w.add(sourceEntry{root: root, rel: rel, name: name, info: info})
	default:
		// Sockets, fifos and device nodes cannot be archived; game-server work
		// directories legitimately contain unix sockets. Matches fsutil.Copy.
		return nil
	}
}

func (w *sourceWalker) walkDir(root *os.Root, rel, name string, info os.FileInfo, symlinkDepth int) error {
	root, rel, err := w.enter(root, rel, info)
	if err != nil {
		return err
	}

	// The "." source contributes its children only; storing "." itself would
	// produce a useless root entry.
	if name != "." {
		if err := w.add(sourceEntry{root: root, rel: rel, name: name, info: info}); err != nil {
			return err
		}
	}

	dirEntries, err := fs.ReadDir(root.FS(), rel)
	if err != nil {
		return errors.Wrapf(err, "failed to read directory %q", rel)
	}

	for _, child := range dirEntries {
		childName := child.Name()
		if name != "." {
			childName = path.Join(name, child.Name())
		}

		if err := w.walk(root, path.Join(rel, child.Name()), childName, symlinkDepth); err != nil {
			return err
		}
	}

	return nil
}

// enter returns where to read the directory rel of root from. A game server
// directory is read through a root of its own from there on; any other
// directory stays where it is.
func (w *sourceWalker) enter(root *os.Root, rel string, info os.FileInfo) (*os.Root, string, error) {
	if rel == "." || !w.confiner.IsServerDir(info) {
		return root, rel, nil
	}

	if known := w.scopeOf(info); known != nil {
		// A link back to the directory the walk is already confined to keeps
		// its path, and with it the limit os.Root puts on a chain of links.
		if known == root {
			return root, rel, nil
		}

		return known, ".", nil
	}

	inner, err := root.OpenRoot(rel)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed to open game server directory %q", rel)
	}

	inner, err = w.adopt(inner, true)
	if err != nil {
		return nil, "", err
	}

	return inner, ".", nil
}
