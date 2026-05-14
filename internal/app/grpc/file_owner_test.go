package grpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests cover the owner-propagation paths in HandleFileWrite and
// handleMkdirOp on non-root test runners — chown itself is a no-op there
// (osowner gates on isRootUser), so the assertions focus on:
//   - the handler must not return an error when owner fields are supplied,
//   - the file/dir is still created with the requested mode,
//   - existing parent directories must keep their ownership untouched
//     (we cannot observe chown directly, but we can observe no failure).

func TestHandleFileWrite_WithOwner_WritesFileWithRequestedMode(t *testing.T) {
	workDir := t.TempDir()
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileWrite(context.Background(), "req-1", &pb.FileWriteRequest{
		Path:       "sub/dir/file.txt",
		Content:    []byte("hello"),
		Mode:       0o600,
		CreateDirs: true,
		OwnerUser:  "gameap",
	})

	require.NoError(t, err)
	require.True(t, resp.Success, "handler returned error: %s", resp.Error)

	written := filepath.Join(workDir, "sub", "dir", "file.txt")
	info, statErr := os.Stat(written)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"requested mode must be honored, not overridden by daemon defaults")
}

func TestHandleFileWrite_EmptyOwner_StillWorks(t *testing.T) {
	workDir := t.TempDir()
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileWrite(context.Background(), "req-2", &pb.FileWriteRequest{
		Path:       "f.txt",
		Content:    []byte("z"),
		Mode:       0o644,
		CreateDirs: false,
	})

	require.NoError(t, err)
	require.True(t, resp.Success, "empty owner must not break the write path: %s", resp.Error)
}

func TestHandleMkdirOp_WithOwner_CreatesDirectoryTree(t *testing.T) {
	workDir := t.TempDir()
	h := NewGRPCFileHandler(workDir)

	resp, err := h.handleMkdirOp("req-3", &pb.MkdirParams{
		Path:      "a/b/c",
		Recursive: true,
		Mode:      0o755,
		OwnerUser: "gameap",
	})

	require.NoError(t, err)
	require.True(t, resp.Success, "mkdir returned error: %s", resp.Error)

	for _, segment := range []string{"a", "a/b", "a/b/c"} {
		info, statErr := os.Stat(filepath.Join(workDir, segment))
		require.NoError(t, statErr, "segment %s must exist", segment)
		assert.True(t, info.IsDir(), "segment %s must be a directory", segment)
	}
}

func TestHandleMkdirOp_NonRecursive_OnlyTargetSegmentNew(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "existing"), 0o755))
	h := NewGRPCFileHandler(workDir)

	resp, err := h.handleMkdirOp("req-4", &pb.MkdirParams{
		Path:      "existing/new",
		Recursive: false,
		Mode:      0o750,
		OwnerUser: "gameap",
	})

	require.NoError(t, err)
	require.True(t, resp.Success, "non-recursive mkdir of a single new segment must succeed: %s", resp.Error)
}
