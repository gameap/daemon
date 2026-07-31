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

// maxRarDictBytes caps the LZ window rardecode allocates up front from the
// archive header. Its own default is 4 GiB, which a crafted RAR can demand in
// a single allocation; WinRAR tops out at 32 MiB for the presets that produce
// real-world archives, so this leaves generous headroom.
const maxRarDictBytes = 256 << 20

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

	if err := s.acc.checkEntryCount(len(zr.File)); err != nil {
		return err
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

// putSymlinkFrom materializes a symlink whose target is stored as the entry
// body — the convention zip, 7z and RAR4 share.
func putSymlinkFrom(r io.Reader, name string, s *sink) error {
	// One byte past the cap, so an oversized target is reported instead of
	// silently becoming a truncated link.
	target, err := io.ReadAll(io.LimitReader(r, maxLinkTargetBytes+1))
	if err != nil {
		return errors.Wrapf(err, "failed to read symlink entry %q", name)
	}
	if len(target) > maxLinkTargetBytes {
		return errors.Errorf("symlink entry %q exceeds the %d byte target limit", name, maxLinkTargetBytes)
	}

	return s.putSymlink(name, string(target))
}

func extractZipSymlink(f *zip.File, s *sink) error {
	rc, err := f.Open()
	if err != nil {
		return errors.Wrapf(err, "failed to open zip entry %q", f.Name)
	}
	defer rc.Close()

	return putSymlinkFrom(rc, f.Name, s)
}

func extractTar(ctx context.Context, archiveFile *os.File, comp compression, s *sink) error {
	stream, err := decompressReader(archiveFile, comp)
	if err != nil {
		return err
	}
	defer stream.Close()

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
	defer stream.Close()

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
		return wrapArchiveReadErr(err, "7z")
	}

	if err := s.acc.checkEntryCount(len(zr.File)); err != nil {
		return err
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

		if err := extract7zEntry(f, s); err != nil {
			// A 7z archive can keep its header readable while only the entry
			// data is encrypted, so "password required" first surfaces here and
			// still has to reach the API as ErrArchiveEncrypted.
			if encrypted7z(err) {
				return errors.Wrap(ErrArchiveEncrypted, "7z archive")
			}

			return err
		}
	}

	return nil
}

func extract7zEntry(f *sevenzip.File, s *sink) error {
	rc, err := f.Open()
	if err != nil {
		return errors.Wrapf(err, "failed to open 7z entry %q", f.Name)
	}
	defer rc.Close()

	// Like zip, a unix symlink is stored as an entry whose body is the link
	// target.
	if f.Mode()&os.ModeSymlink != 0 {
		return putSymlinkFrom(rc, f.Name, s)
	}

	return s.putFile(f.Name, f.Mode(), rc)
}

func extractRar(ctx context.Context, archiveFile *os.File, s *sink) error {
	rr, err := rardecode.NewReader(archiveFile, rardecode.MaxDictionarySize(maxRarDictBytes))
	if err != nil {
		return wrapArchiveReadErr(err, "rar")
	}

	for {
		hdr, err := rr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return wrapArchiveReadErr(err, "rar")
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

		if hdr.Mode()&os.ModeSymlink != 0 {
			if err := extractRarSymlink(hdr, rr, s); err != nil {
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

// extractRarSymlink materializes a RAR4 unix symlink entry. RAR5 redirection
// records are not exposed by rardecode, so those symlinks are still extracted
// as files.
func extractRarSymlink(hdr *rardecode.FileHeader, rr io.Reader, s *sink) error {
	return putSymlinkFrom(rr, hdr.Name, s)
}

// wrapArchiveReadErr turns a decoder failure into the operation error. Both
// decoders can tell "this archive is encrypted" apart from "this archive is
// broken", and the API needs that distinction to prompt for a password
// instead of showing a generic read failure.
func wrapArchiveReadErr(err error, format string) error {
	if errors.Is(err, rardecode.ErrArchiveEncrypted) || encrypted7z(err) {
		return errors.Wrapf(ErrArchiveEncrypted, "%s archive", format)
	}

	return errors.Wrapf(err, "failed to read %s archive", format)
}

// encrypted7z reports a sevenzip failure caused by missing decryption. The
// decoder hands its read errors out as *ReadError, so the target has to be the
// pointer type — matching the value never fires.
func encrypted7z(err error) bool {
	var readErr *sevenzip.ReadError

	return errors.As(err, &readErr) && readErr.Encrypted
}
