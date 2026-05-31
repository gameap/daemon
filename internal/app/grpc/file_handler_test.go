package grpc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGRPCFileHandler_ReadWriteWithinWorkDir(t *testing.T) {
	workDir := t.TempDir()
	h := NewGRPCFileHandler(workDir)
	ctx := context.Background()

	wresp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
		Path: "sub/f.txt", Content: []byte("hello"), Mode: 0o644, CreateDirs: true,
	})
	require.NoError(t, err)
	require.True(t, wresp.Success, wresp.Error)

	rresp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{Path: "sub/f.txt"})
	require.NoError(t, err)
	require.True(t, rresp.Success, rresp.Error)
	assert.Equal(t, []byte("hello"), rresp.Content)
}

func TestGRPCFileHandler_TraversalRejected(t *testing.T) {
	workDir := t.TempDir()
	h := NewGRPCFileHandler(workDir)

	resp, err := h.HandleFileRead(context.Background(), "r", &pb.FileReadRequest{Path: "../etc/passwd"})

	require.NoError(t, err)
	require.False(t, resp.Success)
	assert.Contains(t, resp.Error, "outside work directory")
}

// TestGRPCFileHandler_SymlinkEscapeBlocked is the regression anchor for this
// change: a symlink inside the work directory that points outside it (the kind
// an unprivileged game-server user can create) must not let any operation
// escape. This fails on the old string-based ResolvePath and passes with
// os.Root confinement.
func TestGRPCFileHandler_SymlinkEscapeBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privilege on Windows")
	}

	base := t.TempDir()
	workDir := filepath.Join(base, "work")
	secret := filepath.Join(base, "secret")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(secret, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(secret, "passwd"), []byte("TOPSECRET"), 0o644))

	require.NoError(t, os.Symlink(secret, filepath.Join(workDir, "escape")))
	require.NoError(t, os.Symlink(filepath.Join(secret, "passwd"), filepath.Join(workDir, "escape_file")))

	h := NewGRPCFileHandler(workDir)
	ctx := context.Background()

	t.Run("read_through_symlink_blocked", func(t *testing.T) {
		resp, err := h.HandleFileRead(ctx, "r", &pb.FileReadRequest{Path: "escape_file"})
		require.NoError(t, err)
		assert.False(t, resp.Success, "must not read a file outside the work directory")
		assert.NotEqual(t, []byte("TOPSECRET"), resp.Content)
	})

	t.Run("write_through_symlink_blocked", func(t *testing.T) {
		resp, err := h.HandleFileWrite(ctx, "w", &pb.FileWriteRequest{
			Path: "escape/evil.txt", Content: []byte("x"), Mode: 0o644,
		})
		require.NoError(t, err)
		assert.False(t, resp.Success, "must not write through an escaping symlink")
		_, statErr := os.Stat(filepath.Join(secret, "evil.txt"))
		assert.ErrorIs(t, statErr, os.ErrNotExist, "file must not be created outside the work dir")
	})

	t.Run("list_through_symlink_blocked", func(t *testing.T) {
		resp, err := h.HandleFileList(ctx, "l", &pb.FileListRequest{Path: "escape"})
		require.NoError(t, err)
		assert.False(t, resp.Success, "must not list a directory outside the work directory")
	})

	t.Run("delete_does_not_remove_secret", func(t *testing.T) {
		_, err := h.HandleFileOperation(ctx, &pb.FileOperationRequest{
			RequestId: "d",
			Operation: pb.FileOperationType_FILE_OPERATION_TYPE_DELETE,
			Parameters: &pb.FileOperationRequest_DeleteParams{
				DeleteParams: &pb.DeleteParams{Path: "escape_file"},
			},
		})
		require.NoError(t, err)
		_, statErr := os.Stat(filepath.Join(secret, "passwd"))
		require.NoError(t, statErr, "deleting the symlink must never delete its target")
	})

	t.Run("copy_does_not_exfiltrate_secret", func(t *testing.T) {
		_, err := h.HandleFileOperation(ctx, &pb.FileOperationRequest{
			RequestId: "c",
			Operation: pb.FileOperationType_FILE_OPERATION_TYPE_COPY,
			Parameters: &pb.FileOperationRequest_CopyParams{
				CopyParams: &pb.CopyParams{Source: "escape_file", Destination: "copied"},
			},
		})
		require.NoError(t, err)

		rresp, rerr := h.HandleFileRead(ctx, "rc", &pb.FileReadRequest{Path: "copied"})
		require.NoError(t, rerr)
		if rresp.Success {
			assert.NotContains(t, string(rresp.Content), "TOPSECRET",
				"a copy must never materialize the secret inside the work dir")
		}
	})
}
