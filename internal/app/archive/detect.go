package archive

import (
	"bytes"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/errors"

	pb "github.com/gameap/gameap/pkg/proto"
)

// headerPeekBytes is one tar block: enough for every container signature below
// and for the "ustar" marker a tar header carries at offset 257.
const headerPeekBytes = 512

// tarMagicOffset is where a POSIX tar header stores "ustar".
const tarMagicOffset = 257

var tarMagic = []byte("ustar")

// extensionFormats maps a file suffix onto the format it conventionally names.
// Longest suffix wins, so ".tar.gz" is matched before ".gz".
var extensionFormats = []struct {
	suffix string
	format pb.ArchiveFormat
}{
	{".tar.gz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ},
	{".tar.bz2", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2},
	{".tar.xz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ},
	{".tar.zst", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD},
	{".tgz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ},
	{".tbz2", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2},
	{".tbz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2},
	{".txz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ},
	{".tzst", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD},
	{".tar", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR},
	{".zip", pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP},
	{".7z", pb.ArchiveFormat_ARCHIVE_FORMAT_7Z},
	{".rar", pb.ArchiveFormat_ARCHIVE_FORMAT_RAR},
	{".gz", pb.ArchiveFormat_ARCHIVE_FORMAT_GZ},
	{".bz2", pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2},
	{".xz", pb.ArchiveFormat_ARCHIVE_FORMAT_XZ},
	{".zst", pb.ArchiveFormat_ARCHIVE_FORMAT_ZSTD},
}

// formatFromExtension resolves a format from a file name, returning
// ARCHIVE_FORMAT_UNSPECIFIED when no known suffix matches.
func formatFromExtension(name string) pb.ArchiveFormat {
	lower := strings.ToLower(path.Base(name))

	for _, e := range extensionFormats {
		if strings.HasSuffix(lower, e.suffix) {
			return e.format
		}
	}

	return pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED
}

// containerFromMagic recognizes the self-describing container formats.
func containerFromMagic(head []byte) (pb.ArchiveFormat, bool) {
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")),
		bytes.HasPrefix(head, []byte("PK\x05\x06")),
		bytes.HasPrefix(head, []byte("PK\x07\x08")):
		return pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP, true
	case bytes.HasPrefix(head, []byte("7z\xbc\xaf\x27\x1c")):
		return pb.ArchiveFormat_ARCHIVE_FORMAT_7Z, true
	case bytes.HasPrefix(head, []byte("Rar!\x1a\x07")):
		return pb.ArchiveFormat_ARCHIVE_FORMAT_RAR, true
	case looksLikeTar(head):
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR, true
	default:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, false
	}
}

// compressionFromMagic recognizes the stream compressors. They say nothing
// about whether a tar sits inside, which is resolved separately.
func compressionFromMagic(head []byte) (compression, bool) {
	switch {
	case bytes.HasPrefix(head, []byte("\x1f\x8b")):
		return compGzip, true
	case bytes.HasPrefix(head, []byte("BZh")):
		return compBzip2, true
	case bytes.HasPrefix(head, []byte("\xfd7zXZ\x00")):
		return compXz, true
	case bytes.HasPrefix(head, []byte("\x28\xb5\x2f\xfd")):
		return compZstd, true
	default:
		return compNone, false
	}
}

func looksLikeTar(head []byte) bool {
	if len(head) < tarMagicOffset+len(tarMagic) {
		return false
	}

	return bytes.Equal(head[tarMagicOffset:tarMagicOffset+len(tarMagic)], tarMagic)
}

// detectFormat resolves ARCHIVE_FORMAT_UNSPECIFIED for extraction the way the
// proto describes: by magic bytes, falling back to the file extension. The
// file offset is restored before returning, so the caller can read from the
// start regardless of how much was consumed while sniffing.
func detectFormat(f *os.File, name string) (pb.ArchiveFormat, error) {
	defer func() {
		_, _ = f.Seek(0, io.SeekStart)
	}()

	head := make([]byte, headerPeekBytes)
	n, err := f.ReadAt(head, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, errors.Wrapf(
			err, "failed to read header of %q", name,
		)
	}
	head = head[:n]

	if format, ok := containerFromMagic(head); ok {
		return format, nil
	}

	if comp, ok := compressionFromMagic(head); ok {
		return compressedFormat(f, comp), nil
	}

	if format := formatFromExtension(name); format != pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		return format, nil
	}

	return pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED, errors.Errorf(
		"cannot detect the format of archive %q", name,
	)
}

// compressedFormat decides whether a compressed stream carries a tar or a bare
// file by decompressing just the first tar block and looking for its marker.
// A stream that cannot be decompressed is reported as single-file; opening it
// for real will surface the actual error.
func compressedFormat(f *os.File, comp compression) pb.ArchiveFormat {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return singleFormatFor(comp)
	}

	stream, err := decompressReader(f, comp)
	if err != nil {
		return singleFormatFor(comp)
	}
	defer stream.Close()

	head := make([]byte, headerPeekBytes)
	n, err := io.ReadFull(stream, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return singleFormatFor(comp)
	}

	if looksLikeTar(head[:n]) {
		return tarFormatFor(comp)
	}

	return singleFormatFor(comp)
}

func tarFormatFor(comp compression) pb.ArchiveFormat {
	switch comp {
	case compGzip:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ
	case compBzip2:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2
	case compXz:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ
	case compZstd:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD
	default:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_TAR
	}
}

func singleFormatFor(comp compression) pb.ArchiveFormat {
	switch comp {
	case compGzip:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_GZ
	case compBzip2:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2
	case compXz:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_XZ
	default:
		return pb.ArchiveFormat_ARCHIVE_FORMAT_ZSTD
	}
}
