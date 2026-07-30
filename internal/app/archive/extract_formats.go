package archive

import (
	"archive/tar"
	"archive/zip"
	"context"
	"io"
	"os"
	"path"

	"github.com/bodgit/sevenzip"
	rardecode "github.com/nwaples/rardecode/v2"
	"github.com/pkg/errors"

	pb "github.com/gameap/gameap/pkg/proto"
)

// maxLinkTargetBytes caps how much is read for a symlink entry body; real
// link targets are a few hundred bytes at most.
const maxLinkTargetBytes = 1 << 20

func extractEntries(
	ctx context.Context,
	archiveFile *os.File,
	archiveRel string,
	class formatClass,
	format pb.ArchiveFormat,
	s *sink,
) error {
	switch class {
	case classZip:
		return extractZip(ctx, archiveFile, s)
	case classTar:
		return extractTar(ctx, archiveFile, tarCompression(format), s)
	case classSingle:
		return extractSingle(archiveFile, archiveRel, format, s)
	case class7z:
		return extract7z(ctx, archiveFile, s)
	default:
		return extractRar(ctx, archiveFile, s)
	}
}

func extractZip(ctx context.Context, archiveFile *os.File, s *sink) error {
	info, err := archiveFile.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat archive")
	}

	zr, err := zip.NewReader(archiveFile, info.Size())
	if err != nil {
		return errors.Wrap(err, "failed to read zip archive")
	}

	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "extract archive canceled")
		}

		switch mode := f.Mode(); {
		case f.FileInfo().IsDir():
			if err := s.putDir(f.Name, mode); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			if err := extractZipSymlink(f, s); err != nil {
				return err
			}
		default:
			rc, err := f.Open()
			if err != nil {
				return errors.Wrapf(err, "failed to open zip entry %q", f.Name)
			}

			err = s.putFile(f.Name, mode, rc)
			_ = rc.Close()

			if err != nil {
				return err
			}
		}
	}

	return nil
}

func extractZipSymlink(f *zip.File, s *sink) error {
	rc, err := f.Open()
	if err != nil {
		return errors.Wrapf(err, "failed to open zip entry %q", f.Name)
	}
	defer rc.Close()

	target, err := io.ReadAll(io.LimitReader(rc, maxLinkTargetBytes))
	if err != nil {
		return errors.Wrapf(err, "failed to read symlink entry %q", f.Name)
	}

	return s.putSymlink(f.Name, string(target))
}

func extractTar(ctx context.Context, archiveFile *os.File, comp compression, s *sink) error {
	stream, err := decompressReader(archiveFile, comp)
	if err != nil {
		return err
	}

	tr := tar.NewReader(stream)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "failed to read tar archive")
		}

		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "extract archive canceled")
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := s.putDir(hdr.Name, hdr.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := s.putSymlink(hdr.Name, hdr.Linkname); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := s.putFile(hdr.Name, hdr.FileInfo().Mode(), tr); err != nil {
				return err
			}
		case tar.TypeLink:
			if err := extractTarHardlink(hdr, s); err != nil {
				return err
			}
		default:
			// Fifos, device nodes and pax/gnu metadata records are skipped.
		}
	}
}

// extractTarHardlink materializes a hardlink entry as a copy of the already
// extracted link target.
func extractTarHardlink(hdr *tar.Header, s *sink) error {
	target, ok, err := s.safeEntryName(hdr.Linkname)
	if err != nil {
		return err
	}
	if !ok {
		return errors.Errorf("hardlink entry %q has an empty target", hdr.Name)
	}

	src, err := s.root.Open(target)
	if err != nil {
		return errors.Wrapf(err, "failed to open hardlink target %q", hdr.Linkname)
	}
	defer src.Close()

	return s.putFile(hdr.Name, hdr.FileInfo().Mode(), src)
}

// extractSingle handles the gz/bz2/xz/zstd formats: the whole stream is one
// file, named after the archive minus its compression suffix.
func extractSingle(archiveFile *os.File, archiveRel string, format pb.ArchiveFormat, s *sink) error {
	stream, err := decompressReader(archiveFile, singleCompression(format))
	if err != nil {
		return err
	}

	name := singleOutputName(path.Base(archiveRel), format)

	return s.putFile(name, 0, stream)
}

func extract7z(ctx context.Context, archiveFile *os.File, s *sink) error {
	info, err := archiveFile.Stat()
	if err != nil {
		return errors.Wrap(err, "failed to stat archive")
	}

	zr, err := sevenzip.NewReader(archiveFile, info.Size())
	if err != nil {
		return errors.Wrap(err, "failed to read 7z archive")
	}

	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "extract archive canceled")
		}

		if f.FileInfo().IsDir() {
			if err := s.putDir(f.Name, f.Mode()); err != nil {
				return err
			}

			continue
		}

		rc, err := f.Open()
		if err != nil {
			return errors.Wrapf(err, "failed to open 7z entry %q", f.Name)
		}

		err = s.putFile(f.Name, f.Mode(), rc)
		_ = rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}

func extractRar(ctx context.Context, archiveFile *os.File, s *sink) error {
	rr, err := rardecode.NewReader(archiveFile)
	if err != nil {
		return errors.Wrap(err, "failed to read rar archive")
	}

	for {
		hdr, err := rr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "failed to read rar archive")
		}

		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "extract archive canceled")
		}

		if hdr.IsDir {
			if err := s.putDir(hdr.Name, hdr.Mode()); err != nil {
				return err
			}

			continue
		}

		// The rar reader is sequential: putFile consumes the current entry
		// body (or drains it when the entry is skipped).
		if err := s.putFile(hdr.Name, hdr.Mode(), rr); err != nil {
			return err
		}
	}
}
