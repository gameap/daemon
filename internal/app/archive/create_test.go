package archive

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/gameap/gameap/pkg/proto"
)

var roundtripFormats = []struct {
	name   string
	format pb.ArchiveFormat
	ext    string
}{
	{"zip", pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP, "zip"},
	{"tar", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR, "tar"},
	{"tar.gz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ, "tar.gz"},
	{"tar.bz2", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2, "tar.bz2"},
	{"tar.xz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ, "tar.xz"},
	{"tar.zst", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD, "tar.zst"},
}

func TestCreateExtractRoundtrip(t *testing.T) {
	srcFiles := map[string]string{
		"src/a.txt":        "alpha",
		"src/sub/b.txt":    "bravo\nsecond line",
		"src/sub/c empty":  "",
		"src/deep/d/e.txt": "echo",
	}

	for _, tc := range roundtripFormats {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			writeTree(t, workDir, srcFiles)

			progress := &progressRecord{}
			createRes, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath: "out." + tc.ext,
				Format:      tc.format,
				BasePath:    ".",
				Sources:     []string{"src"},
			}, progress.fn())
			require.NoError(t, err)
			assert.Positive(t, createRes.FilesProcessed)
			assert.Positive(t, createRes.BytesProcessed)
			assert.Positive(t, createRes.ArchiveSize)
			assert.Equal(t, createRes.FilesProcessed, progress.files, "progress must track processed entries")
			assert.NotEmpty(t, progress.entries)

			extractRes, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
				ArchivePath:       "out." + tc.ext,
				Destination:       "dst",
				Format:            tc.format,
				CreateDestination: true,
			}, nil)
			require.NoError(t, err)
			assert.Positive(t, extractRes.FilesProcessed)
			assert.Empty(t, extractRes.Skipped)

			want := map[string]string{
				"src/a.txt":        "alpha",
				"src/sub/b.txt":    "bravo\nsecond line",
				"src/sub/c empty":  "",
				"src/deep/d/e.txt": "echo",
			}
			assert.Equal(t, want, readTree(t, filepath.Join(workDir, "dst")))
		})
	}
}

func TestCreateExtractRoundtripBasePath(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{
		"base/one.txt":     "1",
		"base/dir/two.txt": "2",
	})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.tar",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		BasePath:    "base",
		Sources:     []string{"."},
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "out.tar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"one.txt":     "1",
		"dir/two.txt": "2",
	}, readTree(t, filepath.Join(workDir, "dst")))
}

var singleFormats = []struct {
	name   string
	format pb.ArchiveFormat
	ext    string
}{
	{"gz", pb.ArchiveFormat_ARCHIVE_FORMAT_GZ, "gz"},
	{"bz2", pb.ArchiveFormat_ARCHIVE_FORMAT_BZ2, "bz2"},
	{"xz", pb.ArchiveFormat_ARCHIVE_FORMAT_XZ, "xz"},
	{"zst", pb.ArchiveFormat_ARCHIVE_FORMAT_ZSTD, "zst"},
}

func TestSingleFileRoundtrip(t *testing.T) {
	for _, tc := range singleFormats {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			writeTree(t, workDir, map[string]string{"data.bin": "single file payload"})

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath: "data.bin." + tc.ext,
				Format:      tc.format,
				BasePath:    ".",
				Sources:     []string{"data.bin"},
			}, nil)
			require.NoError(t, err)

			_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
				ArchivePath:       "data.bin." + tc.ext,
				Destination:       "dst",
				Format:            tc.format,
				CreateDestination: true,
			}, nil)
			require.NoError(t, err)

			assert.Equal(t, map[string]string{"data.bin": "single file payload"},
				readTree(t, filepath.Join(workDir, "dst")))
		})
	}
}

func TestSingleFileExtractNoSuffix(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"payload": "no suffix payload"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "compressed",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
		BasePath:    ".",
		Sources:     []string{"payload"},
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "compressed",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"compressed.out": "no suffix payload"},
		readTree(t, filepath.Join(workDir, "dst")))
}

func TestCreateErrors(t *testing.T) {
	t.Run("unspecified format without a known extension", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"a.txt": "a"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.bundle",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED,
			Sources:     []string{"a.txt"},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no known extension")
	})

	t.Run("extract-only formats", func(t *testing.T) {
		for _, format := range []pb.ArchiveFormat{
			pb.ArchiveFormat_ARCHIVE_FORMAT_7Z,
			pb.ArchiveFormat_ARCHIVE_FORMAT_RAR,
		} {
			workDir := t.TempDir()
			writeTree(t, workDir, map[string]string{"a.txt": "a"})

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath: "out.arc",
				Format:      format,
				Sources:     []string{"a.txt"},
			}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "extract-only")
		}
	})

	t.Run("empty sources", func(t *testing.T) {
		workDir := t.TempDir()

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{},
		}, nil)
		require.Error(t, err)
	})

	t.Run("existing archive without overwrite", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"a.txt": "a", "out.zip": "old"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{"a.txt"},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")

		content, readErr := os.ReadFile(filepath.Join(workDir, "out.zip"))
		require.NoError(t, readErr)
		assert.Equal(t, "old", string(content), "existing archive must stay untouched")
	})

	t.Run("existing archive with overwrite", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"a.txt": "a", "out.zip": "old"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			Sources:     []string{"a.txt"},
			Overwrite:   true,
		}, nil)
		require.NoError(t, err)
	})

	t.Run("single format with two sources", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"a.txt": "a", "b.txt": "b"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.gz",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
			Sources:     []string{"a.txt", "b.txt"},
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exactly one")
	})

	t.Run("single format with directory source", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"dir/a.txt": "a"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.gz",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
			Sources:     []string{"dir"},
		}, nil)
		require.Error(t, err)
	})

	t.Run("source escapes base path", func(t *testing.T) {
		workDir := t.TempDir()
		writeTree(t, workDir, map[string]string{"a.txt": "a"})

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "out.zip",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			BasePath:    "sub",
			Sources:     []string{"../a.txt"},
		}, nil)
		require.Error(t, err)
	})
}

func TestCreateMaxFilesLimit(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"a.txt": "a", "b.txt": "b"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.zip",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		Sources:     []string{"a.txt", "b.txt"},
		MaxFiles:    1,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max files limit exceeded")

	_, statErr := os.Stat(filepath.Join(workDir, "out.zip"))
	assert.True(t, os.IsNotExist(statErr), "failed create must not leave a partial archive")
}

func TestCreateMaxTotalBytesLimit(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"a.txt": "more than one byte"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:   "out.zip",
		Format:        pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		Sources:       []string{"a.txt"},
		MaxTotalBytes: 1,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max total bytes limit exceeded")
}

// TestCreateExtractHugeMaxTotalBytes pins the byte budget at the very top of
// its range: every streaming copy is capped one byte past what is left, so the
// largest limit a request can ask for must still archive and extract content
// instead of overflowing that cap into a read that yields nothing.
func TestCreateExtractHugeMaxTotalBytes(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/a.txt": "alpha"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:   "out.zip",
		Format:        pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		BasePath:      "src",
		Sources:       []string{"."},
		MaxTotalBytes: math.MaxUint64,
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "out.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
		MaxTotalBytes:     math.MaxUint64,
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"a.txt": "alpha"}, readTree(t, filepath.Join(workDir, "dst")))
}

func TestCreateModeOnArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not supported on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"a.txt": "a"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.zip",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		Sources:     []string{"a.txt"},
		Mode:        0o600,
	}, nil)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(workDir, "out.zip"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestCreateCanceledContext(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"a.txt": "a"})

	_, err := Create(canceledContext(t), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.zip",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		Sources:     []string{"a.txt"},
	}, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCreateCompressionLevels(t *testing.T) {
	levels := map[string]*int32{
		"store":   new(int32),
		"fastest": new(int32(1)),
		"best":    new(int32(9)),
	}

	formats := []struct {
		name   string
		format pb.ArchiveFormat
	}{
		{"zip", pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP},
		{"tar.gz", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ},
		{"tar.bz2", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_BZ2},
		{"tar.zst", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_ZSTD},
	}

	for _, f := range formats {
		for levelName, level := range levels {
			t.Run(f.name+"/"+levelName, func(t *testing.T) {
				workDir := t.TempDir()
				writeTree(t, workDir, map[string]string{"a.txt": "compressible payload payload payload"})

				_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
					ArchivePath:      "out.arc",
					Format:           f.format,
					Sources:          []string{"a.txt"},
					CompressionLevel: level,
				}, nil)
				require.NoError(t, err)

				_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
					ArchivePath:       "out.arc",
					Destination:       "dst",
					Format:            f.format,
					CreateDestination: true,
				}, nil)
				require.NoError(t, err)

				assert.Equal(t, map[string]string{"a.txt": "compressible payload payload payload"},
					readTree(t, filepath.Join(workDir, "dst")))
			})
		}
	}
}

func TestSymlinkRoundtrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	for _, tc := range []struct {
		name   string
		format pb.ArchiveFormat
		ext    string
	}{
		{"zip", pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP, "zip"},
		{"tar", pb.ArchiveFormat_ARCHIVE_FORMAT_TAR, "tar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workDir := t.TempDir()
			writeTree(t, workDir, map[string]string{"src/target.txt": "link me"})
			require.NoError(t, os.Symlink("target.txt", filepath.Join(workDir, "src", "link.txt")))

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath: "out." + tc.ext,
				Format:      tc.format,
				Sources:     []string{"src"},
			}, nil)
			require.NoError(t, err)

			_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
				ArchivePath:       "out." + tc.ext,
				Destination:       "dst",
				Format:            tc.format,
				CreateDestination: true,
			}, nil)
			require.NoError(t, err)

			link, err := os.Readlink(filepath.Join(workDir, "dst", "src", "link.txt"))
			require.NoError(t, err)
			assert.Equal(t, "target.txt", link)
		})
	}
}

func TestCreateFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/target.txt": "followed"})
	require.NoError(t, os.Symlink("target.txt", filepath.Join(workDir, "src", "link.txt")))

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:    "out.tar",
		Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:        []string{"src"},
		FollowSymlinks: true,
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "out.tar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	info, err := os.Lstat(filepath.Join(workDir, "dst", "src", "link.txt"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular(), "followed symlink must be archived as a regular file")

	content, err := os.ReadFile(filepath.Join(workDir, "dst", "src", "link.txt"))
	require.NoError(t, err)
	assert.Equal(t, "followed", string(content))
}

func TestCreateDeepDirectoryNesting(t *testing.T) {
	workDir := t.TempDir()

	deep := "src"
	for range maxFollowDepth + 10 {
		deep += "/d"
	}
	writeTree(t, workDir, map[string]string{deep + "/file.txt": "deep"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "out.tar",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:     []string{"src"},
	}, nil)
	require.NoError(t, err, "deep ordinary directory nesting must not trigger the symlink depth limit")
}

func TestCreateFollowSymlinksDepthLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/file.txt": "data"})
	require.NoError(t, os.Symlink(".", filepath.Join(workDir, "src", "loop")))

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:    "out.tar",
		Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:        []string{"src"},
		FollowSymlinks: true,
	}, nil)
	require.Error(t, err)
	// The daemon's own hop limit fires at maxFollowDepth+1; the OS may reject
	// the accumulated path earlier with its own symlink expansion limit.
	assert.True(t,
		strings.Contains(err.Error(), "symlink nesting too deep") ||
			strings.Contains(err.Error(), "too many levels of symbolic links"),
		"unexpected error: %v", err)
}

// TestWalkSourceSymlinkDepthLimit checks the daemon's own hop counter
// deterministically: following one more symlink past maxFollowDepth fails
// before any OS-level resolution is attempted.
func TestWalkSourceSymlinkDepthLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/file.txt": "data"})
	require.NoError(t, os.Symlink(".", filepath.Join(workDir, "src", "loop")))

	root, err := os.OpenRoot(workDir)
	require.NoError(t, err)
	defer root.Close()

	w := &sourceWalker{
		ctx:    context.Background(),
		root:   root,
		limits: &walkLimits{follow: true, maxEntries: defaultMaxFilesLimit()},
	}

	err = w.walk("src/loop", "src/loop", maxFollowDepth)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlink nesting too deep")
}

func defaultMaxFilesLimit() uint64 {
	return uint64(defaultMaxFiles)
}

// TestCreateFollowSymlinksFanOutIsBounded builds a symlink DAG that stays under
// os.Root's own 8-hop limit, so no single path is ever rejected while the
// number of distinct paths grows exponentially. The entry limit has to apply
// during the walk, not only when entries are written.
func TestCreateFollowSymlinksFanOutIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	const levels = 7

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{
		filepath.ToSlash(filepath.Join("src", fmt.Sprintf("d%d", levels), "leaf.txt")): "leaf",
	})

	for lvl := range levels {
		dir := filepath.Join(workDir, "src", fmt.Sprintf("d%d", lvl))
		require.NoError(t, os.MkdirAll(dir, 0o755))

		for k := range 3 {
			require.NoError(t, os.Symlink(
				fmt.Sprintf("../d%d", lvl+1),
				filepath.Join(dir, fmt.Sprintf("l%d", k)),
			))
		}
	}

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:    "out.tar",
		Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		Sources:        []string{"src"},
		FollowSymlinks: true,
		MaxFiles:       50,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max files limit exceeded")
}

func TestSourceWalkerRespectsContext(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/a.txt": "a"})

	root, err := os.OpenRoot(workDir)
	require.NoError(t, err)
	defer root.Close()

	w := &sourceWalker{
		ctx:    canceledContext(t),
		root:   root,
		limits: &walkLimits{maxEntries: defaultMaxFilesLimit()},
	}

	err = w.walk("src", "src", 0)
	require.ErrorIs(t, err, context.Canceled)
}

// TestCreateExcludesItself covers the archive sitting inside the tree being
// walked: it must not be archived into itself while it is being written.
func TestCreateExcludesItself(t *testing.T) {
	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/a.txt": "alpha"})

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "src/out.tar",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		BasePath:    "src",
		Sources:     []string{"."},
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "src/out.tar",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_TAR,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"a.txt": "alpha"}, readTree(t, filepath.Join(workDir, "dst")))
}

// TestCreateExcludesItselfThroughSymlink covers the same exclusion reached
// through a symlink: with follow_symlinks the walker resolves the link to the
// archive it is writing, which the identity comparison has to catch after the
// resolution as well as before it.
func TestCreateExcludesItselfThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	workDir := t.TempDir()
	writeTree(t, workDir, map[string]string{"src/a.txt": "alpha"})
	require.NoError(t, os.Symlink("out.zip", filepath.Join(workDir, "src", "self.zip")))

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:    "src/out.zip",
		Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		BasePath:       "src",
		Sources:        []string{"."},
		FollowSymlinks: true,
		// Bounds what archiving the archive into itself could produce if the
		// exclusion ever regresses.
		MaxTotalBytes: 1 << 20,
	}, nil)
	require.NoError(t, err)

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "src/out.zip",
		Destination:       "dst",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{"a.txt": "alpha"}, readTree(t, filepath.Join(workDir, "dst")))
}

// TestFormatDetection checks the proto contract that an unset format is
// resolved from the archive itself, including telling tar.gz from a bare gz —
// the two share a magic number and differ only in what the stream contains.
func TestFormatDetection(t *testing.T) {
	detect := func(t *testing.T, name, ext, source string, format pb.ArchiveFormat) {
		t.Helper()

		t.Run(name, func(t *testing.T) {
			workDir := t.TempDir()
			writeTree(t, workDir, map[string]string{"src/a.txt": "alpha"})

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath: "out." + ext,
				Format:      format,
				Sources:     []string{source},
			}, nil)
			require.NoError(t, err)

			res, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
				ArchivePath:       "out." + ext,
				Destination:       "dst",
				Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED,
				CreateDestination: true,
			}, nil)
			require.NoError(t, err)
			assert.Equal(t, format, res.Format)
		})
	}

	for _, tc := range roundtripFormats {
		detect(t, tc.name, tc.ext, "src", tc.format)
	}

	for _, tc := range singleFormats {
		detect(t, tc.name, tc.ext, "src/a.txt", tc.format)
	}
}
