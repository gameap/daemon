// Package archive creates and extracts archives (zip, tar family, single-file
// compression, 7z, rar) on behalf of API requests. Every caller-supplied path
// is resolved with fsutil.RootRel and accessed through an *os.Root opened at
// the work directory, so no operation can escape the work directory — os.Root
// refuses symlink and ".." resolution outside of it. Extracted archive entries
// are additionally rejected lexically when their path is absolute or starts
// with ".." (zip-slip), because the API contract requires the friendly error.
//
// Extraction feeds attacker-controlled bytes to third-party format decoders,
// so the caller is expected to run it under a recover (see the gRPC archive
// handler). One residual risk cannot be handled here: bodgit/sevenzip sizes
// several slices directly from 7z header counts and validates them only
// against MaxUint32, so a crafted header can request an allocation large
// enough for the Go runtime to abort the process — a throw that no recover
// can intercept. It exposes no option to bound this; the entry-count check
// below runs only after its reader is already constructed.
package archive

import (
	"math"

	"github.com/pkg/errors"

	"github.com/gameap/daemon/internal/app/osowner"
	pb "github.com/gameap/gameap/pkg/proto"
)

const (
	// defaultMaxTotalBytes caps the uncompressed payload of one operation when
	// the request does not set a limit (decompression-bomb protection).
	defaultMaxTotalBytes uint64 = 10 << 30 // 10 GiB
	// defaultMaxFiles caps the number of entries of one operation when the
	// request does not set a limit.
	defaultMaxFiles uint32 = 100_000

	// maxFollowDepth bounds symlink-following recursion during create. It is a
	// backstop only: os.Root refuses to resolve a path crossing more than
	// rootMaxSymlinks (8) links, so in practice that limit fires first.
	maxFollowDepth = 40
)

// ErrArchiveEncrypted reports an archive the daemon cannot open because it is
// password protected. The API distinguishes this from a corrupt archive to
// prompt the user for a password.
var ErrArchiveEncrypted = errors.New("archive is encrypted, password required")

// Result summarizes one Create or Extract call.
type Result struct {
	FilesProcessed int64
	BytesProcessed int64 // uncompressed bytes
	ArchiveSize    int64 // produced archive when creating, source archive when extracting
	Skipped        []string
	// Format the operation actually used, which differs from the requested one
	// when the request left it unspecified and the daemon resolved it.
	Format pb.ArchiveFormat
}

// ProgressFunc is invoked after each processed entry (it may be nil).
// Totals are intentionally not reported: the proto allows 0 = unknown.
type ProgressFunc func(filesProcessed, bytesProcessed int64, currentEntry string)

// accumulator tracks running counters, enforces the limits and reports
// progress for both create and extract.
type accumulator struct {
	files    int64
	bytes    int64
	maxBytes uint64
	maxFiles uint64
	progress ProgressFunc
}

func newAccumulator(maxBytes uint64, maxFiles uint32, progress ProgressFunc) *accumulator {
	if maxBytes == 0 {
		maxBytes = defaultMaxTotalBytes
	}
	// maxBytes arrives as uint64 while the counters run on int64; clamp so the
	// conversions in bytesLeft and addEntry cannot wrap negative. The clamp
	// stops one below MaxInt64 because every streaming copy reads bytesLeft()+1
	// bytes: at MaxInt64 that sum overflows into a negative limit, which
	// io.LimitReader reports as immediate EOF and would silently reduce every
	// entry to nothing.
	if maxBytes > math.MaxInt64-1 {
		maxBytes = math.MaxInt64 - 1
	}
	if maxFiles == 0 {
		maxFiles = defaultMaxFiles
	}

	return &accumulator{maxBytes: maxBytes, maxFiles: uint64(maxFiles), progress: progress}
}

// bytesLeft is used to cap streaming copies one byte past the limit so an
// oversized payload is detected instead of silently truncated.
func (a *accumulator) bytesLeft() int64 {
	left := int64(a.maxBytes) - a.bytes
	if left < 0 {
		return 0
	}

	return left
}

// checkEntryCount rejects an archive whose entry count is known up front and
// already exceeds the limit, so nothing is written before the operation fails.
func (a *accumulator) checkEntryCount(n int) error {
	if uint64(n) > a.maxFiles {
		return errors.Errorf("max files limit exceeded (%d)", a.maxFiles)
	}

	return nil
}

// addEntry records one processed entry with n uncompressed content bytes.
func (a *accumulator) addEntry(name string, n int64) error {
	a.files++
	if uint64(a.files) > a.maxFiles {
		return errors.Errorf("max files limit exceeded (%d)", a.maxFiles)
	}

	a.bytes += n
	if a.bytes > int64(a.maxBytes) {
		return errors.Errorf("max total bytes limit exceeded (%d)", a.maxBytes)
	}

	if a.progress != nil {
		a.progress(a.files, a.bytes, name)
	}

	return nil
}

func ownerOptions(user string, uid, gid int32) osowner.Options {
	return osowner.Options{User: user, UID: uid, GID: gid}
}
