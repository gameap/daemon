package archive

import (
	"io"
	"os"

	"github.com/pkg/errors"

	pb "github.com/gameap/gameap/pkg/proto"
)

// createSingle writes the gz/bz2/xz/zstd single-file formats: the archive is
// the compressed stream of exactly one regular file.
func createSingle(
	root *os.Root,
	archiveFile io.Writer,
	entries []sourceEntry,
	p *pb.CreateArchiveParams,
	format pb.ArchiveFormat,
	acc *accumulator,
) error {
	if len(entries) != 1 || !entries[0].info.Mode().IsRegular() {
		return errors.Errorf(
			"archive format %s requires exactly one regular file source", format,
		)
	}

	e := entries[0]

	stream, closer, err := compressWriter(archiveFile, singleCompression(format), p.CompressionLevel)
	if err != nil {
		return err
	}

	closed := false
	defer func() {
		if closer != nil && !closed {
			_ = closer.Close()
		}
	}()

	n, err := copySource(root, e.rel, stream, acc.bytesLeft())
	if err != nil {
		return err
	}

	if closer != nil {
		closed = true
		if err := closer.Close(); err != nil {
			return errors.Wrap(err, "failed to finish compressor stream")
		}
	}

	return acc.addEntry(e.name, n)
}
