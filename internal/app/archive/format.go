package archive

import (
	"compress/gzip"
	"strings"

	"github.com/pkg/errors"

	dsbzip2 "github.com/dsnet/compress/bzip2"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/klauspost/compress/zstd"
)

// formatClass groups archive formats by the code path that handles them.
type formatClass int

const (
	classZip    formatClass = iota + 1
	classTar                // plain tar and tar wrapped into a compressor stream
	classSingle             // gz/bz2/xz/zstd compressing one bare file
	class7z
	classRar
)

// compression identifies the stream compressor wrapped around a tar stream or
// used for a single-file format.
type compression int

const (
	compNone compression = iota
	compGzip
	compBzip2
	compXz
	compZstd
)

func classify(format pb.ArchiveFormat) (formatClass, error) {
	switch format {
	case pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP:
		return classZip, nil
	case pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ,
		pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2,
		pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ,
		pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD:
		return classTar, nil
	case pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
		pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2,
		pb.ArchiveFormat_ARCHIVE_FORMAT_XZ,
		pb.ArchiveFormat_ARCHIVE_FORMAT_ZSTD:
		return classSingle, nil
	case pb.ArchiveFormat_ARCHIVE_FORMAT_7Z:
		return class7z, nil
	case pb.ArchiveFormat_ARCHIVE_FORMAT_RAR:
		return classRar, nil
	default:
		return 0, errors.Errorf("unsupported archive format: %s", format)
	}
}

// classifyForCreate rejects formats the daemon cannot write.
func classifyForCreate(format pb.ArchiveFormat) (formatClass, error) {
	if format == pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED {
		return 0, errors.New("archive format is unspecified")
	}

	class, err := classify(format)
	if err != nil {
		return 0, err
	}

	if class == class7z || class == classRar {
		return 0, errors.Errorf("archive format %s is extract-only", format)
	}

	return class, nil
}

func tarCompression(format pb.ArchiveFormat) compression {
	switch format {
	case pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ:
		return compGzip
	case pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2:
		return compBzip2
	case pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ:
		return compXz
	case pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD:
		return compZstd
	default:
		return compNone
	}
}

func singleCompression(format pb.ArchiveFormat) compression {
	switch format {
	case pb.ArchiveFormat_ARCHIVE_FORMAT_GZ:
		return compGzip
	case pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2:
		return compBzip2
	case pb.ArchiveFormat_ARCHIVE_FORMAT_XZ:
		return compXz
	default:
		return compZstd
	}
}

// singleSuffix maps a single-file format to its conventional file suffix.
func singleSuffix(format pb.ArchiveFormat) string {
	switch format {
	case pb.ArchiveFormat_ARCHIVE_FORMAT_GZ:
		return ".gz"
	case pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2:
		return ".bz2"
	case pb.ArchiveFormat_ARCHIVE_FORMAT_XZ:
		return ".xz"
	default:
		return ".zst"
	}
}

// singleOutputName derives the extracted file name for a single-file format:
// the archive base name minus its compression suffix, or "<base>.out" when
// the archive name carries no known suffix.
func singleOutputName(archiveName string, format pb.ArchiveFormat) string {
	if base, ok := strings.CutSuffix(archiveName, singleSuffix(format)); ok && base != "" {
		return base
	}

	return archiveName + ".out"
}

// gzipLevel maps the proto compression level onto compress/gzip levels:
// unset = format default, 0 = store (NoCompression), 1..9 passed through.
func gzipLevel(level *int32) int {
	if level == nil {
		return gzip.DefaultCompression
	}

	return clampLevel(*level)
}

// bzip2Level maps the proto compression level onto dsnet bzip2 levels. The
// format cannot store, so a store request degrades to the fastest level.
func bzip2Level(level *int32) int {
	if level == nil {
		return dsbzip2.DefaultCompression
	}
	if *level == 0 {
		return dsbzip2.BestSpeed
	}

	return clampLevel(*level)
}

// zstdLevel maps the proto compression level onto klauspost zstd encoder
// levels. The format cannot store, so a store request degrades to the
// fastest level.
func zstdLevel(level *int32) zstd.EncoderLevel {
	if level == nil {
		return zstd.SpeedDefault
	}

	switch {
	case *level <= 3:
		return zstd.SpeedFastest
	case *level <= 6:
		return zstd.SpeedDefault
	case *level <= 8:
		return zstd.SpeedBetterCompression
	default:
		return zstd.SpeedBestCompression
	}
}

// flateLevel maps the proto compression level onto compress/flate levels for
// zip deflate entries. xz is not mapped at all: the xz format has no
// compression levels (only dictionary presets), so the level is ignored there.
func flateLevel(level *int32) int {
	if level == nil {
		return gzip.DefaultCompression
	}

	return clampLevel(*level)
}

func clampLevel(level int32) int {
	if level < 0 {
		return 0
	}
	if level > 9 {
		return 9
	}

	return int(level)
}
