package archive

import (
	"context"
	"io"
	"math"
	"os"

	"github.com/pkg/errors"

	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
)

// TreeOptions tune ExtractTree.
type TreeOptions struct {
	// Owner is given to every entry the archive creates.
	Owner    osowner.Options
	Progress ProgressFunc
	// Format is what the archive is unpacked as when the caller was told. Left
	// unspecified, it is taken from the content and, failing that, from the
	// name.
	Format pb.ArchiveFormat
}

// SkippedLink is a symlink entry ExtractTree left out.
type SkippedLink struct {
	Name   string
	Target string
	Reason string
}

// IsArchiveName reports whether name ends the way the archives this package
// unpacks are conventionally named. A caller that is handed a file without
// being told what it is uses it to tell an archive to unpack from a file to
// keep as it is: the content cannot say, since a jar or a pk3 is a zip to
// anything that looks inside.
func IsArchiveName(name string) bool {
	return formatFromExtension(name) != pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED
}

// ExtractTree unpacks archiveFile into the directory root is opened at.
//
// It is Extract for a caller that holds both ends already: the archive as an
// open file, wherever it came from, and the destination as the root every
// entry is written through. Nothing is resolved by a path here, so what the
// entries can reach is decided by how the caller opened root. name is what the
// archive is called; its format is taken from the content and, failing that,
// from the name, unless the options give one.
//
// The archive is an installation source the operator configured, and the tree
// it lands in may hold what a tenant put there. The entries are therefore
// placed as follows:
//
//   - a file entry replaces the file or the symlink standing under its name:
//     the link is taken away and the entry written in its place, not into what
//     the link leads to;
//   - a directory entry merges into a directory, also through a symlink the
//     root follows to one. A symlink the root does not follow, because it leads
//     out of the destination or nowhere, fails the run and is named in the
//     error: it is neither followed nor taken away;
//   - a symlink entry is created when its target stays inside the destination.
//     One that does not is left out and reported in Result.SkippedLinks, so that
//     an archive carrying an absolute link still installs;
//   - a directory standing where the archive has a file or a symlink fails the
//     run: a tree is not removed to make room for an entry.
//
// These rules describe a tree that holds still. Whoever owns it can change it
// while the archive is unpacked, and what holds then is the root alone: an
// entry lands inside the directory root is opened at, wherever in it.
//
// File entries keep the permission bits of the archive. So do the directories
// a tar lists, the destination itself among them when the tar has an entry for
// it ("./"), and also a directory that was there before: a directory the
// archive keeps closed to the other users of the node stays closed, whatever
// the files in it allow.
//
// A directory the archive keeps its owner from writing into (0555) takes that
// mode once the archive is through, and stays open to its owner until then:
// its entries come after its own, and a daemon that is not root could not
// create them otherwise.
//
// Every other directory is created with 0755, and one that was there keeps its
// mode. That is a directory the archive only implies, and any directory of the
// other formats: those may record no permissions, and their readers make them
// up then (0777 for a directory of a zip made on Windows). An entry without
// any permission bits is taken to record none either, as the archives written
// without them have it: a file is then created with 0644, a directory with
// 0755, and the destination keeps its mode.
//
// A run that fails takes away what it had put where nothing stood before: the
// files, links and directories that are new to the destination. Whoever tries
// another source next does not find the leftovers of this one among its files.
// What the run had replaced by then is not brought back and stays as the
// archive has it, and a directory that stays is given its mode as after a run
// that went through.
//
// The limits Extract applies to the request of a panel user are not applied.
func ExtractTree(
	ctx context.Context,
	archiveFile *os.File,
	name string,
	root *os.Root,
	opts TreeOptions,
) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.Wrap(err, "extract archive canceled")
	}

	archiveInfo, err := archiveFile.Stat()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat archive %q", name)
	}

	if _, err := archiveFile.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Wrapf(err, "failed to rewind archive %q", name)
	}

	format := opts.Format
	if format == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		if format, err = detectFormat(archiveFile, name); err != nil {
			return nil, err
		}
	}

	class, err := classify(format)
	if err != nil {
		return nil, err
	}

	s := &sink{
		root:            root,
		dest:            ".",
		policy:          pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
		preserveFiles:   true,
		preserveDirs:    class == classTar,
		owner:           opts.Owner,
		acc:             newAccumulator(math.MaxUint64, math.MaxUint32, opts.Progress),
		mergeLinkedDirs: true,
		skipUnsafeLinks: true,
		keepDirs:        true,
		applyDestMode:   true,
		undoCreated:     true,
	}

	extractErr := extractEntries(ctx, archiveFile, name, class, format, s)

	// As in Extract: the links are settled against the finished tree and the
	// directory modes after them, also after a run that failed.
	linkErr := s.revalidateLinks()

	// Before the modes: an entry is taken out of a directory that can still be
	// written to.
	if extractErr != nil || linkErr != nil {
		s.removeCreated()
	}

	modeErr := s.applyDirModes(".")

	if extractErr != nil {
		return nil, extractErr
	}
	if linkErr != nil {
		return nil, linkErr
	}
	if modeErr != nil {
		return nil, errors.WithMessagef(modeErr, "failed to settle the directory modes of archive %q", name)
	}

	return &Result{
		FilesProcessed: s.acc.files,
		BytesProcessed: s.acc.bytes,
		ArchiveSize:    archiveInfo.Size(),
		Format:         format,
		SkippedLinks:   s.unsafeLinks,
	}, nil
}
