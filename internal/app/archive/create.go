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
// Every source is placed by the confiner on its own, and the walk keeps to the
// same boundary as it goes: a game server directory it enters from above is
// read through a root opened at it, and a link it follows from above is read
// where the link ends, in the game server directory that place lies in. Above
// is the work directory as much as a game server directory that holds another
// one. A tree packed for the node, or for a game server with others inside it,
// is thus confined the way a tree packed for a single game server is: os.Root
// refuses a link leading out of the game server directory it stands in,
// whichever directory the request started from.
type sourceWalker struct {
	ctx      context.Context
	confiner *fsutil.Confiner
	limits   *walkLimits
	entries  []sourceEntry
	scopes   []*sourceScope
}

// sourceScope is a root the sources are read through.
type sourceScope struct {
	root *os.Root
	dir  os.FileInfo
	// path is where root stands, relative to the work directory.
	path string
	// placed is the placement the scope was made of.
	placed *fsutil.Confined
	// confined is set for a root standing in a game server directory that
	// holds no other one. Nothing read through it leads out of that directory,
	// so the walk can skip placement of non-boundaries below it. That holds for the
	// game servers known when the root was placed, and the walk outlasts it:
	// the flag is read through sourceWalker.confined.
	confined bool
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

	if _, err := w.adopt(base, false); err != nil {
		return nil, err
	}

	for _, src := range sources {
		srcRel, err := fsutil.RootRel(src)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid source %q", src)
		}

		// The base path is a directory to pack from. Anything else is an entry
		// of it: a link is packed as a link unless the request has links
		// followed, and a directory is entered by the walk like any other.
		target := fsutil.TargetEntry
		if srcRel == "." {
			target = fsutil.TargetTree
		}

		placed, err := w.confiner.Resolve(path.Join(baseRel, srcRel), target)
		if err != nil {
			return nil, err
		}

		scope, err := w.adopt(placed, true)
		if err != nil {
			return nil, err
		}

		if err := w.walk(scope, placed.Rel, srcRel, 0); err != nil {
			return nil, err
		}
	}

	return w.entries, nil
}

// adopt returns the scope to read the directory of placed through: the one
// already open there, or one made of placed, whose root is then kept. The
// sources of one request mostly share a directory, and a link can lead to a
// game server directory any number of times; neither may cost a descriptor
// each.
func (w *sourceWalker) adopt(placed *fsutil.Confined, owned bool) (*sourceScope, error) {
	dir, err := placed.Root.Stat(".")
	if err != nil {
		if owned {
			_ = placed.Root.Close()
		}

		return nil, errors.Wrapf(err, "failed to stat source directory %q", placed.Scope)
	}

	if known := w.scopeOf(dir); known != nil {
		if owned {
			_ = placed.Root.Close()
		}

		// One directory can be placed under several names, and a later
		// placement knows of game servers an earlier one did not: the scope is
		// confined only while every placement finds it so.
		known.confined = known.confined && placed.Innermost

		return known, nil
	}

	scope := &sourceScope{
		root:     placed.Root,
		dir:      dir,
		path:     placed.Scope,
		placed:   placed,
		confined: placed.Innermost,
		owned:    owned,
	}

	w.scopes = append(w.scopes, scope)

	return scope, nil
}

// confined reports whether scope still stands in a game server directory that
// holds no other one. The daemon learns of game servers while the walk lasts,
// so the question is put anew before every directory entered and every link
// followed. A scope found to hold a game server directory stays unconfined:
// from then on its directories are asked about one by one.
func (w *sourceWalker) confined(scope *sourceScope) bool {
	if scope.confined && !w.confiner.StillInnermost(scope.placed) {
		scope.confined = false
	}

	return scope.confined
}

func (w *sourceWalker) scopeOf(dir os.FileInfo) *sourceScope {
	for _, scope := range w.scopes {
		if os.SameFile(scope.dir, dir) {
			return scope
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

func (w *sourceWalker) walk(scope *sourceScope, rel, name string, symlinkDepth int) error {
	if err := w.ctx.Err(); err != nil {
		return errors.Wrap(err, "create archive canceled")
	}

	root := scope.root

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

		into, intoRel, err := w.cross(scope, rel, w.confiner.IsServerDir(info))
		if err != nil {
			return err
		}

		if into != nil {
			return w.walk(into, intoRel, name, symlinkDepth)
		}
	}

	switch {
	case info.IsDir():
		return w.walkDir(scope, rel, name, info, symlinkDepth)
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

// cross returns where to go on reading rel of scope, when that lies behind a
// boundary: the scope of the game server directory rel is, or the followed
// link rel ends in, and the path there. What crosses no boundary is read
// through the root it stands in, and nothing is returned. A boundary already
// observed by identity takes precedence over the confined shortcut.
//
// The root of scope would follow a link wherever it leads below it, into the
// middle of a game server directory as well, and the links the tenant keeps
// there would then be followed through that root too, out of the game server
// directory again.
func (w *sourceWalker) cross(scope *sourceScope, rel string, serverBoundary bool) (*sourceScope, string, error) {
	if !serverBoundary && w.confined(scope) {
		return nil, "", nil
	}

	placed, err := w.confiner.Resolve(path.Join(scope.path, rel), fsutil.TargetTree)
	if err != nil {
		return nil, "", err
	}

	into, err := w.adopt(placed, true)
	if err != nil {
		return nil, "", err
	}

	if into == scope {
		return nil, "", nil
	}

	return into, placed.Rel, nil
}

func (w *sourceWalker) walkDir(scope *sourceScope, rel, name string, info os.FileInfo, symlinkDepth int) error {
	scope, rel, err := w.enter(scope, rel, info)
	if err != nil {
		return err
	}

	root := scope.root

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

		if err := w.walk(scope, path.Join(rel, child.Name()), childName, symlinkDepth); err != nil {
			return err
		}
	}

	return nil
}

// enter returns where to read the directory rel of scope from. A game server
// directory is read through a root of its own from there on; any other
// directory stays where it is.
func (w *sourceWalker) enter(scope *sourceScope, rel string, info os.FileInfo) (*sourceScope, string, error) {
	if rel == "." || !w.confiner.IsServerDir(info) {
		return scope, rel, nil
	}

	into, intoRel, err := w.cross(scope, rel, true)
	if err != nil {
		return nil, "", errors.WithMessagef(err, "failed to enter game server directory %q", path.Join(scope.path, rel))
	}

	if into == nil {
		return scope, rel, nil
	}

	return into, intoRel, nil
}
