package archive

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gameap/daemon/internal/app/fsutil"
	pb "github.com/gameap/gameap/pkg/proto"
)

// TestCreateExtractThroughAllowedSymlinkTarget covers gameap#109 for archives:
// a server directory that is a symlink onto another drive works for both
// operations once the drive is listed, and an archive may live in a different
// root than the files it holds.
func TestCreateExtractThroughAllowedSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	workDir := filepath.Join(base, "work")
	drive := filepath.Join(base, "disk2", "servers")
	serverDir := filepath.Join(drive, "x")
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "servers"), 0o755))
	require.NoError(t, os.MkdirAll(serverDir, 0o755))

	files := map[string]string{
		"cfg/server.cfg": "hostname x",
		"maps/de.bsp":    "map",
	}
	writeTree(t, serverDir, files)
	require.NoError(t, os.Symlink(serverDir, filepath.Join(workDir, "servers", "x")))

	ctx := context.Background()
	create := &pb.CreateArchiveParams{
		ArchivePath: "servers/x/backup.zip",
		Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		BasePath:    "servers/x",
		Sources:     []string{"cfg", "maps"},
	}

	_, err = Create(ctx, workDir, create, nil)
	require.Error(t, err, "without the option the link stays refused, by os.Root itself")
	assert.Contains(t, err.Error(), "path escapes from parent")

	allowed := fsutil.WithAllowedSymlinkTargets([]string{drive})

	res, err := Create(ctx, workDir, create, nil, allowed)
	require.NoError(t, err)
	assert.Positive(t, res.FilesProcessed)
	assert.FileExists(t, filepath.Join(serverDir, "backup.zip"))

	_, err = Extract(ctx, workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "servers/x/backup.zip",
		Destination:       "servers/x/restore",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil, allowed)
	require.NoError(t, err)
	assert.Equal(t, files, readTree(t, filepath.Join(serverDir, "restore")))

	// The archive in the work directory, the destination on the other drive.
	require.NoError(t, os.Rename(
		filepath.Join(serverDir, "backup.zip"), filepath.Join(workDir, "servers", "backup.zip"),
	))

	_, err = Extract(ctx, workDir, &pb.ExtractArchiveParams{
		ArchivePath:       "servers/backup.zip",
		Destination:       "servers/x/fromwork",
		Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		CreateDestination: true,
	}, nil, allowed)
	require.NoError(t, err)
	assert.Equal(t, files, readTree(t, filepath.Join(serverDir, "fromwork")))
}

// TestCreateSourcesMustShareTheBaseRoot: sources are walked through the root
// the base path resolved to, so a source that a link takes to another root
// is reported rather than silently skipped.
func TestCreateSourcesMustShareTheBaseRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}

	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	workDir := filepath.Join(base, "work")
	drive := filepath.Join(base, "disk2", "servers")
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "servers"), 0o755))
	writeTree(t, filepath.Join(drive, "x"), map[string]string{"f.txt": "x"})
	require.NoError(t, os.Symlink(filepath.Join(drive, "x"), filepath.Join(workDir, "servers", "x")))

	_, err = Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath:    "backup.zip",
		Format:         pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
		BasePath:       ".",
		Sources:        []string{"servers/x"},
		FollowSymlinks: true,
	}, nil, fsutil.WithAllowedSymlinkTargets([]string{drive}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "same storage root")
}
