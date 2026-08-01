package archive

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"io"
	"os"
	"strings"

	"github.com/pkg/errors"

	dsbzip2 "github.com/dsnet/compress/bzip2"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// compressWriter wraps w with the requested stream compressor. The returned
// closer must be closed before w itself is closed; it closes only the
// compressor stream.
func compressWriter(w io.Writer, comp compression, level *int32) (io.Writer, io.Closer, error) {
	switch comp {
	case compGzip:
		gw, err := gzip.NewWriterLevel(w, gzipLevel(level))
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to init gzip writer")
		}

		return gw, gw, nil
	case compBzip2:
		bw, err := dsbzip2.NewWriter(w, &dsbzip2.WriterConfig{Level: bzip2Level(level)})
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to init bzip2 writer")
		}

		return bw, bw, nil
	case compXz:
		// The xz format has no compression levels (only filter presets), so a
		// requested level is silently ignored here.
		xw, err := xz.NewWriter(w)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to init xz writer")
		}

		return xw, xw, nil
	case compZstd:
		zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstdLevel(level)))
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to init zstd writer")
		}

		return zw, zw, nil
	default:
		return w, nil, nil
	}
}

// decompressReader wraps r with the matching stream decompressor. Closing
// the returned reader releases the decompressor; it never closes r itself.
func decompressReader(r io.Reader, comp compression) (io.ReadCloser, error) {
	switch comp {
	case compGzip:
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, errors.Wrap(err, "failed to init gzip reader")
		}

		return gr, nil
	case compBzip2:
		// stdlib on the read path: it is continuously fuzzed and reports
		// corruption as an error. dsnet/compress is kept for writing only —
		// it is the one that exposes compression levels, but it decodes with
		// panic-as-control-flow behind a recover that re-raises anything it
		// does not recognize, which is not what should face untrusted input.
		return io.NopCloser(bzip2.NewReader(r)), nil
	case compXz:
		xr, err := xz.NewReader(r)
		if err != nil {
			return nil, errors.Wrap(err, "failed to init xz reader")
		}

		return io.NopCloser(xr), nil
	case compZstd:
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, errors.Wrap(err, "failed to init zstd reader")
		}

		return zr.IOReadCloser(), nil
	default:
		return io.NopCloser(r), nil
	}
}

// createTar writes entries into a tar stream, optionally through a stream
// compressor (tar.gz, tar.bz2, tar.xz, tar.zst).
func createTar(
	ctx context.Context,
	root *os.Root,
	w io.Writer,
	entries []sourceEntry,
	p *pb.CreateArchiveParams,
	comp compression,
	acc *accumulator,
) error {
	stream, closer, err := compressWriter(w, comp, p.CompressionLevel)
	if err != nil {
		return err
	}

	closed := false
	defer func() {
		if closer != nil && !closed {
			_ = closer.Close()
		}
	}()

	tw := tar.NewWriter(stream)

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "create archive canceled")
		}

		hdr, err := tar.FileInfoHeader(e.info, e.link)
		if err != nil {
			return errors.Wrapf(err, "failed to build header for %q", e.name)
		}

		hdr.Name = e.name
		if e.info.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return errors.Wrapf(err, "failed to write header for %q", e.name)
		}

		var n int64
		if hdr.Typeflag == tar.TypeReg {
			n, err = copySource(root, e.rel, tw, acc.bytesLeft())
			if err != nil {
				return err
			}
		}

		if err := acc.addEntry(e.name, n); err != nil {
			return err
		}
	}

	if err := tw.Close(); err != nil {
		return errors.Wrap(err, "failed to finish tar archive")
	}

	if closer != nil {
		closed = true
		if err := closer.Close(); err != nil {
			return errors.Wrap(err, "failed to finish compressor stream")
		}
	}

	return nil
}
