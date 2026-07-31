package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/gameap/gameap/pkg/proto"
)

func TestExtract7zFixture(t *testing.T) {
	workDir := t.TempDir()
	copyFixture(t, workDir, "test.7z")

	res, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "test.7z",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_7Z,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.FilesProcessed)

	assert.Equal(t, map[string]string{
		"bar": "bar\n",
		"foo": "foo\n",
	}, readTree(t, filepath.Join(workDir, "dst")))
}

func TestExtractRarFixture(t *testing.T) {
	workDir := t.TempDir()
	copyFixture(t, workDir, "test.rar")

	res, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "test.rar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_RAR,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.FilesProcessed)

	assert.Equal(t, map[string]string{
		"hello.txt":         "Hello, RAR!\n",
		"subdir/nested.txt": "nested rar content\n",
	}, readTree(t, filepath.Join(workDir, "dst")))
}

// buildZip writes a zip archive with the given raw entry names (no
// sanitization, so malicious names can be constructed).
func buildZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = w.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

// zipEntry is one entry for buildZipModes, carrying a full os.FileMode so
// symlink and directory entries can be constructed.
type zipEntry struct {
	name string
	mode os.FileMode
	body string
}

// buildZipModes writes a zip preserving entry order, so entries that depend on
// earlier ones (a symlink into a directory unpacked before it) behave the way
// they would in a real archive.
func buildZipModes(t *testing.T, path string, entries []zipEntry) {
	t.Helper()

	f, err := os.Create(path)
	require.NoError(t, err)

	zw := zip.NewWriter(f)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Store}
		hdr.SetMode(e.mode)

		w, createErr := zw.CreateHeader(hdr)
		require.NoError(t, createErr)
		_, writeErr := w.Write([]byte(e.body))
		require.NoError(t, writeErr)
	}

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

// TestExtractSymlinkChainEscape covers a target that a purely lexical check
// accepts: "a/b/l/../../../x" cleans to "dst/x", but "a/b/l" is itself a link
// to "../..", so the kernel walks the path out of the work directory.
func TestExtractSymlinkChainEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "chain.zip"), []zipEntry{
		{name: "a/b/", mode: os.ModeDir | 0o755},
		{name: "a/b/l", mode: os.ModeSymlink | 0o777, body: "../.."},
		{name: "esc", mode: os.ModeSymlink | 0o777, body: "a/b/l/../../../x"},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "chain.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"esc"`)

	link, readErr := os.Readlink(filepath.Join(workDir, "dst", "esc"))
	assert.Error(t, readErr, "escaping symlink must not be created, points at %q", link)
}

// TestExtractSymlinkChainWithinDestination is the counterpart: the same kind of
// chain must keep working as long as it stays inside the destination.
func TestExtractSymlinkChainWithinDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "chain.zip"), []zipEntry{
		{name: "a/b/", mode: os.ModeDir | 0o755},
		{name: "a/b/l", mode: os.ModeSymlink | 0o777, body: "../.."},
		{name: "ok", mode: os.ModeSymlink | 0o777, body: "a/b/l/payload.txt"},
		{name: "payload.txt", mode: 0o644, body: "payload"},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "chain.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "dst", "ok"))
	require.NoError(t, err)
	assert.Equal(t, "payload", string(content), "a chain resolving inside the destination must still work")
}

// TestExtractSymlinkEscapeThroughLaterEntry covers the same escape built the
// other way round: "esc" is stored while "a/b" is still missing, so its target
// resolves literally to "dst/c", and only the entry after it turns "a/b" into
// the link that walks the finished tree out of the work directory.
func TestExtractSymlinkEscapeThroughLaterEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "chain.zip"), []zipEntry{
		{name: "esc", mode: os.ModeSymlink | 0o777, body: "a/b/../../c"},
		{name: "a/b", mode: os.ModeSymlink | 0o777, body: ".."},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "chain.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.Error(t, err)

	link, readErr := os.Readlink(filepath.Join(workDir, "dst", "esc"))
	assert.Error(t, readErr, "escaping symlink must not survive the run, points at %q", link)
}

// TestExtractSymlinkEscapeThroughOverwrittenDirectory is the overwrite variant:
// "a/b" is a directory when "esc" is checked against it and a symlink by the
// time the run ends.
func TestExtractSymlinkEscapeThroughOverwrittenDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "chain.zip"), []zipEntry{
		{name: "a/b/", mode: os.ModeDir | 0o755},
		{name: "esc", mode: os.ModeSymlink | 0o777, body: "a/b/../../c"},
		{name: "a/b", mode: os.ModeSymlink | 0o777, body: ".."},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "chain.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
		ConflictPolicy:    pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
	}, nil)
	require.Error(t, err)

	link, readErr := os.Readlink(filepath.Join(workDir, "dst", "esc"))
	assert.Error(t, readErr, "escaping symlink must not survive the run, points at %q", link)
}

// TestExtractSymlinkEscapeThroughSymlinkedParent covers the link that is not
// stored where its name says: "s1" and "s2" both resolve back to the
// destination, so os.Root puts "l" directly under it and its "../.." reaches
// above the work directory, however deep the entry name looks.
func TestExtractSymlinkEscapeThroughSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "parent.zip"), []zipEntry{
		{name: "s1", mode: os.ModeSymlink | 0o777, body: "."},
		{name: "s1/s2", mode: os.ModeSymlink | 0o777, body: "."},
		{name: "s1/s2/l", mode: os.ModeSymlink | 0o777, body: "../../y"},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "parent.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"s1/s2/l"`)

	link, readErr := os.Readlink(filepath.Join(workDir, "dst", "l"))
	assert.Error(t, readErr, "escaping symlink must not be created, points at %q", link)
}

// TestExtractOversizedSymlinkTarget pins that a target past the cap is reported
// instead of silently truncated into a link pointing somewhere else entirely.
func TestExtractOversizedSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildZipModes(t, filepath.Join(workDir, "big.zip"), []zipEntry{
		{name: "link", mode: os.ModeSymlink | 0o777, body: strings.Repeat("a", maxLinkTargetBytes+1)},
	})

	_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "big.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target limit")
}

func TestExtractCorruptedArchives(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture string
		format  pb.ArchiveFormat
	}{
		{"7z", "test.7z", pb.ArchiveFormat_ARCHIVE_FORMAT_7Z},
		{"rar", "test.rar", pb.ArchiveFormat_ARCHIVE_FORMAT_RAR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			copyFixture(t, workDir, tc.fixture)

			p := filepath.Join(workDir, tc.fixture)
			data, err := os.ReadFile(p)
			require.NoError(t, err)

			// Keep the signature so the decoder commits to parsing, then feed
			// it garbage: this must surface as an error, never a panic.
			for i := 8; i < len(data); i++ {
				data[i] ^= 0xFF
			}
			require.NoError(t, os.WriteFile(p, data, 0o644))

			_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
				ArchivePath:       tc.fixture,
				Destination:       "dst",
				Format:            tc.format,
				CreateDestination: true,
			}, nil)
			require.Error(t, err)
		})
	}
}

// rar4Entry is one stored (uncompressed) RAR4 file entry; attr is the unix
// st_mode value reported in the header (host OS is always unix here).
type rar4Entry struct {
	name string
	attr uint32
	data []byte
}

// buildRar4 writes a minimal RAR4 archive with stored entries: marker block,
// main header, one file header per entry and an end-of-archive block. There
// is no RAR encoder in the module dependencies, so the bytes are assembled
// by hand following the layout rardecode parses.
func buildRar4(t *testing.T, path string, entries []rar4Entry) {
	t.Helper()

	var buf bytes.Buffer
	buf.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00}) // RAR4 marker block
	writeRar4Block(&buf, 0x73, 0, make([]byte, 6))              // main archive header

	for _, e := range entries {
		hdr := make([]byte, 0, 25+len(e.name))
		hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(e.data))) // packed size
		hdr = binary.LittleEndian.AppendUint32(hdr, uint32(len(e.data))) // unpacked size
		hdr = append(hdr, 3)                                             // host OS: unix
		hdr = binary.LittleEndian.AppendUint32(hdr, crc32.ChecksumIEEE(e.data))
		hdr = binary.LittleEndian.AppendUint32(hdr, 0) // modification time (dos format)
		hdr = append(hdr, 20)                          // minimum rar version to extract
		hdr = append(hdr, 0x30)                        // method: store
		hdr = binary.LittleEndian.AppendUint16(hdr, uint16(len(e.name)))
		hdr = binary.LittleEndian.AppendUint32(hdr, e.attr)
		hdr = append(hdr, e.name...)

		writeRar4Block(&buf, 0x74, 0x8000, hdr) // 0x8000: entry data follows the header
		buf.Write(e.data)
	}

	writeRar4Block(&buf, 0x7B, 0, nil) // end of archive

	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

// writeRar4Block appends one RAR4 block: crc16 (low bits of the CRC32 over
// type..end), type, flags, header size and the header body.
func writeRar4Block(buf *bytes.Buffer, btype byte, flags uint16, data []byte) {
	body := make([]byte, 0, 5+len(data))
	body = append(body, btype)
	body = binary.LittleEndian.AppendUint16(body, flags)
	body = binary.LittleEndian.AppendUint16(body, uint16(7+len(data)))
	body = append(body, data...)

	buf.Write(binary.LittleEndian.AppendUint16(nil, uint16(crc32.ChecksumIEEE(body))))
	buf.Write(body)
}

func TestExtractRarSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	buildRar4(t, filepath.Join(workDir, "links.rar"), []rar4Entry{
		{name: "target.txt", attr: 0x81A4, data: []byte("linked content\n")}, // regular 0644
		{name: "link.txt", attr: 0xA1FF, data: []byte("target.txt")},         // symlink 0777
	})

	res, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "links.rar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_RAR,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), res.FilesProcessed)

	link, err := os.Readlink(filepath.Join(workDir, "dst", "link.txt"))
	require.NoError(t, err)
	assert.Equal(t, "target.txt", link, "rar symlink entry must be extracted as a symlink, not a regular file")

	content, err := os.ReadFile(filepath.Join(workDir, "dst", "target.txt"))
	require.NoError(t, err)
	assert.Equal(t, "linked content\n", string(content))
}

func TestExtractZipSlip(t *testing.T) {
	t.Run("dotdot entry", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(workDir, "dst"), 0o755))
		buildZip(t, filepath.Join(workDir, "evil.zip"), map[string]string{"../evil.txt": "pwned"})

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "evil.zip",
			Destination: "dst",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the destination")

		_, statErr := os.Stat(filepath.Join(workDir, "evil.txt"))
		assert.True(t, os.IsNotExist(statErr), "zip-slip file must not be created outside the destination")
	})

	t.Run("absolute entry", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(workDir, "dst"), 0o755))
		buildZip(t, filepath.Join(workDir, "evil.zip"), map[string]string{"/abs.txt": "pwned"})

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "evil.zip",
			Destination: "dst",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the destination")
	})

	t.Run("backslash dotdot entry", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(workDir, "dst"), 0o755))
		buildZip(t, filepath.Join(workDir, "evil.zip"), map[string]string{`..\evil.txt`: "pwned"})

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "evil.zip",
			Destination: "dst",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes the destination")
	})
}

func TestExtractConflictPolicy(t *testing.T) {
	buildArchive := func(t *testing.T, workDir string) {
		t.Helper()
		writeTree(t, workDir, map[string]string{"src/exists.txt": "new", "src/fresh.txt": "fresh"})
		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{"src"},
		}, nil)
		require.NoError(t, err)
	}

	prepareDst := func(t *testing.T, workDir string) {
		t.Helper()
		writeTree(t, workDir, map[string]string{"dst/src/exists.txt": "old"})
	}

	extract := func(t *testing.T, workDir string, policy pb.ArchiveConflictPolicy) (*Result, error) {
		t.Helper()

		return Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:    "out.zip",
			Destination:    "dst",
			Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			ConflictPolicy: policy,
		}, nil)
	}

	conflictingContent := func(t *testing.T, workDir string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(workDir, "dst", "src", "exists.txt"))
		require.NoError(t, err)

		return string(content)
	}

	t.Run("error is the default", func(t *testing.T) {
		workDir := t.TempDir()
		buildArchive(t, workDir)
		prepareDst(t, workDir)

		_, err := extract(t, workDir, pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_UNSPECIFIED)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
		assert.Equal(t, "old", conflictingContent(t, workDir))
	})

	t.Run("error policy", func(t *testing.T) {
		workDir := t.TempDir()
		buildArchive(t, workDir)
		prepareDst(t, workDir)

		_, err := extract(t, workDir, pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_ERROR)
		require.Error(t, err)
		assert.Equal(t, "old", conflictingContent(t, workDir))
	})

	t.Run("skip policy", func(t *testing.T) {
		workDir := t.TempDir()
		buildArchive(t, workDir)
		prepareDst(t, workDir)

		res, err := extract(t, workDir, pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_SKIP)
		require.NoError(t, err)
		assert.Equal(t, []string{"src/exists.txt"}, res.Skipped)
		assert.Equal(t, "old", conflictingContent(t, workDir))
		assert.Equal(t, "fresh", readTree(t, filepath.Join(workDir, "dst"))["src/fresh.txt"],
			"non-conflicting entries must still be extracted")
	})

	t.Run("overwrite policy", func(t *testing.T) {
		workDir := t.TempDir()
		buildArchive(t, workDir)
		prepareDst(t, workDir)

		res, err := extract(t, workDir, pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE)
		require.NoError(t, err)
		assert.Empty(t, res.Skipped)
		assert.Equal(t, "new", conflictingContent(t, workDir))
	})
}

func TestExtractLimits(t *testing.T) {
	setup := func(t *testing.T, workDir string) {
		t.Helper()
		writeTree(t, workDir, map[string]string{"src/a.txt": "aaaa", "src/b.txt": "bbbb"})
		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{"src"},
		}, nil)
		require.NoError(t, err)
	}

	t.Run("max total bytes", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:       "out.zip",
			Destination:       "dst",
			Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			CreateDestination: true,
			MaxTotalBytes:     1,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max total bytes limit exceeded")
	})

	t.Run("max files", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:       "out.zip",
			Destination:       "dst",
			Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			CreateDestination: true,
			MaxFiles:          1,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "max files limit exceeded")
	})
}

func TestExtractDestination(t *testing.T) {
	setup := func(t *testing.T, workDir string) {
		t.Helper()
		writeTree(t, workDir, map[string]string{"a.txt": "a"})
		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{"a.txt"},
		}, nil)
		require.NoError(t, err)
	}

	t.Run("missing destination without create flag", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "out.zip",
			Destination: "missing",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("missing destination with create flag", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:       "out.zip",
			Destination:       "missing/nested",
			Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			CreateDestination: true,
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"a.txt": "a"}, readTree(t, filepath.Join(workDir, "missing", "nested")))
	})

	t.Run("destination is a file", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "out.zip",
			Destination: "a.txt",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})

	t.Run("unspecified format is detected", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		res, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:       "out.zip",
			Destination:       "dst",
			Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED,
			CreateDestination: true,
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP, res.Format)
		assert.Equal(t, map[string]string{"a.txt": "a"}, readTree(t, filepath.Join(workDir, "dst")))
	})
}

func TestExtractModeOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not supported on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"a.txt": "a"})
	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.tar",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:     []string{"a.txt"},
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "out.tar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		CreateDestination: true,
		Mode:              0o600,
	}, nil)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(workDir, "dst", "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "mode must override archive permissions")
}

func TestExtractPreservePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not supported on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"script.sh": "#!/bin/sh\n"})
	require.NoError(t, os.Chmod(filepath.Join(workDir, "script.sh"), 0o750))

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.tar",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:     []string{"script.sh"},
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:         "out.tar",
		Destination:         "dst",
		Format:              pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		CreateDestination:   true,
		PreservePermissions: true,
	}, nil)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(workDir, "dst", "script.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm(), "archive permissions must be preserved")
}

func TestExtractCanceledContext(t *testing.T) {
	workDir := t.TempDir()
	copyFixture(t, workDir, "test.7z")

	_, err := Extract(canceledContext(t), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "test.7z",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_7Z,
		CreateDestination: true,
	}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
