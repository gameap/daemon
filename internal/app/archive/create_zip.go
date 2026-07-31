package archive

import (
	"archive/zip"
	"compress/flate"
	"context"
	"io"
	"os"

	"github.com/pkg/errors"

	pb "github.com/gameap/gameap/pkg/proto"
)

// createZip writes entries into a zip stream. Compression level 0 maps to
// per-entry Store; other levels tune the deflate compressor.
func createZip(
	ctx context.Context,
	root *os.Root,
	w io.Writer,
	entries []sourceEntry,
	p *pb.CreateArchiveParams,
	acc *accumulator,
) error {
	zw := zip.NewWriter(w)

	store := false
	if p.CompressionLevel != nil {
		if *p.CompressionLevel == 0 {
			store = true
		} else {
			level := flateLevel(p.CompressionLevel)
			zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
				return flate.NewWriter(out, level)
			})
		}
	}

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "create archive canceled")
		}

		hdr, err := zip.FileInfoHeader(e.info)
		if err != nil {
			return errors.Wrapf(err, "failed to build header for %q", e.name)
		}

		hdr.Name = e.name
		if e.info.IsDir() {
			hdr.Name += "/"
		}

		if store || e.isSymlink() {
			hdr.Method = zip.Store
		}

		entryWriter, err := zw.CreateHeader(hdr)
		if err != nil {
			return errors.Wrapf(err, "failed to write header for %q", e.name)
		}

		var n int64

		switch {
		case e.info.IsDir():
		case e.isSymlink():
			nw, writeErr := entryWriter.Write([]byte(e.link))
			n = int64(nw)
			if writeErr != nil {
				return errors.Wrapf(writeErr, "failed to write symlink %q", e.name)
			}
		default:
			n, err = copySource(root, e.rel, entryWriter, acc.bytesLeft())
			if err != nil {
				return err
			}
		}

		if err := acc.addEntry(e.name, n); err != nil {
			return err
		}
	}

	if err := zw.Close(); err != nil {
		return errors.Wrap(err, "failed to finish zip archive")
	}

	return nil
}
