package archive

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"runtime"
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

	t.Run("unspecified format", func(t *testing.T) {
		workDir := t.TempDir()
		setup(t, workDir)

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "out.zip",
			Destination: "dst",
			Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_UNSPECIFIED,
		}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unspecified")
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
