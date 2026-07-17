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

func TestResolvePath(t *testing.T) {
	t.Run("relative_path_resolves_under_workdir", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		resolved, err := ResolvePath(workDir, "servers/x")

		require.NoError(t, err)
		assert.Equal(t, filepath.Join(workDir, "servers", "x"), resolved)
	})

	t.Run("empty_path_resolves_to_workdir", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		resolved, err := ResolvePath(workDir, "")

		require.NoError(t, err)
		assert.Equal(t, filepath.Clean(workDir), resolved)
	})

	t.Run("dot_resolves_to_workdir", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		resolved, err := ResolvePath(workDir, ".")

		require.NoError(t, err)
		assert.Equal(t, filepath.Clean(workDir), resolved)
	})

	t.Run("escape_via_dot_dot_is_rejected", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		_, err := ResolvePath(workDir, "../etc/passwd")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside work directory")
	})

	t.Run("forward_slash_leading_separator_is_stripped", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		resolved, err := ResolvePath(workDir, "/servers/x")

		require.NoError(t, err)
		assert.Equal(t, filepath.Join(workDir, "servers", "x"), resolved)
	})

	t.Run("backslash_leading_separator_is_stripped", func(t *testing.T) {
		workDir := filepath.Join(t.TempDir(), "work")

		resolved, err := ResolvePath(workDir, `\servers\x`)

		require.NoError(t, err)

		expected := filepath.Join(workDir, "servers", "x")
		if runtime.GOOS != "windows" {
			expected = filepath.Join(workDir, `servers\x`)
		}

		assert.Equal(t, expected, resolved)
	})

	t.Run("forward_slash_path_with_workdir_prefix_does_not_double", func(t *testing.T) {
		workDir := "/srv/gameap"

		resolved, err := ResolvePath(workDir, "/srv/gameap/servers/x")

		require.NoError(t, err)

		if runtime.GOOS == "windows" {
			assert.Equal(t, filepath.Clean(`/srv/gameap/srv/gameap/servers/x`), resolved)
		} else {
			assert.Equal(t, "/srv/gameap/srv/gameap/servers/x", resolved)
		}
	})

	t.Run("workdir_path_is_cleaned", func(t *testing.T) {
		workDir := "/srv/gameap/"

		resolved, err := ResolvePath(workDir, "servers/x")

		require.NoError(t, err)
		assert.Equal(t, filepath.Join("/srv/gameap", "servers", "x"), resolved)
	})
}

func TestResolvePath_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only path behavior")
	}

	t.Run("absolute_windows_path_with_drive_letter_is_stripped", func(t *testing.T) {
		workDir := `C:\gameap`

		resolved, err := ResolvePath(workDir, `C:\gameap\servers\x`)

		require.NoError(t, err)
		assert.Equal(t, `C:\gameap\gameap\servers\x`, resolved)
	})

	t.Run("absolute_windows_path_lowercase_drive_still_stripped", func(t *testing.T) {
		workDir := `C:\gameap`

		resolved, err := ResolvePath(workDir, `c:\some\where`)

		require.NoError(t, err)
		assert.Equal(t, `C:\gameap\some\where`, resolved)
	})

	t.Run("case_insensitive_containment_check", func(t *testing.T) {
		workDir := `C:\GameAP`

		resolved, err := ResolvePath(workDir, `servers\x`)

		require.NoError(t, err)
		assert.Equal(t, `C:\GameAP\servers\x`, resolved)
	})

	t.Run("forward_slash_input_resolves_via_windows_separator", func(t *testing.T) {
		workDir := `C:\gameap`

		resolved, err := ResolvePath(workDir, "servers/x")

		require.NoError(t, err)
		assert.Equal(t, `C:\gameap\servers\x`, resolved)
	})

	t.Run("dot_dot_escape_still_rejected_on_windows", func(t *testing.T) {
		workDir := `C:\gameap`

		_, err := ResolvePath(workDir, `..\Windows\System32`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside work directory")
	})
}

func TestPathWithin(t *testing.T) {
	t.Run("exact_match_is_within", func(t *testing.T) {
		assert.True(t, pathWithin("/srv/gameap", "/srv/gameap"))
	})

	t.Run("subpath_is_within", func(t *testing.T) {
		assert.True(t, pathWithin(filepath.Join("/srv/gameap", "servers"), "/srv/gameap"))
	})

	t.Run("prefix_without_separator_boundary_is_not_within", func(t *testing.T) {
		assert.False(t, pathWithin("/srv/gameap-other", "/srv/gameap"))
	})

	t.Run("unrelated_path_is_not_within", func(t *testing.T) {
		assert.False(t, pathWithin("/etc/passwd", "/srv/gameap"))
	})

	t.Run("shorter_path_is_not_within", func(t *testing.T) {
		assert.False(t, pathWithin("/srv", "/srv/gameap"))
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
