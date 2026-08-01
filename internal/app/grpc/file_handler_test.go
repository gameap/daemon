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

func TestHandleFileList(t *testing.T) {
	t.Run("recursive_missing_directory_returns_error", func(t *testing.T) {
		h := NewGRPCFileHandler(t.TempDir())

		resp, err := h.HandleFileList(context.Background(), "req-1", &pb.FileListRequest{
			Path:      "does/not/exist",
			Recursive: true,
		})

		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.NotEmpty(t, resp.Error)
		require.Len(t, resp.Files, 0)
	})

	t.Run("flat_missing_directory_returns_error", func(t *testing.T) {
		h := NewGRPCFileHandler(t.TempDir())

		resp, err := h.HandleFileList(context.Background(), "req-2", &pb.FileListRequest{
			Path:      "does/not/exist",
			Recursive: false,
		})

		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.NotEmpty(t, resp.Error)
		require.Len(t, resp.Files, 0)
	})

	t.Run("recursive_empty_directory_returns_empty_success", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, "empty"), 0o755))
		h := NewGRPCFileHandler(workDir)

		resp, err := h.HandleFileList(context.Background(), "req-3", &pb.FileListRequest{
			Path:      "empty",
			Recursive: true,
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Empty(t, resp.Error)
		require.Len(t, resp.Files, 0)
	})

	t.Run("recursive_regular_file_returns_error", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "file.txt"), []byte("data"), 0o644))
		h := NewGRPCFileHandler(workDir)

		resp, err := h.HandleFileList(context.Background(), "req-5", &pb.FileListRequest{
			Path:      "file.txt",
			Recursive: true,
		})

		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "not a directory")
		require.Len(t, resp.Files, 0)
	})

	t.Run("recursive_unreadable_root_returns_error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions are not enforced the same way on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}

		workDir := t.TempDir()
		locked := filepath.Join(workDir, "locked")
		require.NoError(t, os.MkdirAll(locked, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(locked, "file.txt"), []byte("data"), 0o644))
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() {
			_ = os.Chmod(locked, 0o755)
		})

		h := NewGRPCFileHandler(workDir)

		resp, err := h.HandleFileList(context.Background(), "req-6", &pb.FileListRequest{
			Path:      "locked",
			Recursive: true,
		})

		require.NoError(t, err)
		assert.False(t, resp.Success, "a root directory that cannot be read must not answer with an empty success")
		assert.NotEmpty(t, resp.Error)
		require.Len(t, resp.Files, 0)
	})

	t.Run("recursive_existing_directory_lists_entries", func(t *testing.T) {
		workDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(workDir, "sub"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "sub", "file.txt"), []byte("data"), 0o644))
		h := NewGRPCFileHandler(workDir)

		resp, err := h.HandleFileList(context.Background(), "req-4", &pb.FileListRequest{
			Path:      "",
			Recursive: true,
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		require.Len(t, resp.Files, 2)

		paths := make([]string, 0, len(resp.Files))
		for _, f := range resp.Files {
			paths = append(paths, f.Path)
		}
		assert.Contains(t, paths, "sub")
		assert.Contains(t, paths, filepath.Join("sub", "file.txt"))
	})
}
