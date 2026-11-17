package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resolveEntry(t *testing.T, confiner *Confiner, p string) *Confined {
	t.Helper()

	c, err := confiner.Resolve(p, TargetEntry)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func TestRename_WithinOneServerDir(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

	err := Rename(
		resolveEntry(t, confiner, "servers/a/own.txt"),
		resolveEntry(t, confiner, "servers/a/sub/renamed.txt"),
	)

	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "sub", "renamed.txt"))
	require.NoError(t, err)
	assert.Equal(t, "own", string(content))

	_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "own.txt"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestRename_BetweenServerDirs(t *testing.T) {
	t.Parallel()

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

		err := Rename(
			resolveEntry(t, confiner, "servers/a/own.txt"),
			resolveEntry(t, confiner, "servers/b/moved.txt"),
		)

		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "moved.txt"))
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))

		_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "own.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("directory_into_a_node_directory", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

		err := Rename(
			resolveEntry(t, confiner, "servers/a/sub"),
			resolveEntry(t, confiner, "steamcmd/sub"),
		)

		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(workDir, "steamcmd", "sub", "inner.txt"))
		require.NoError(t, err)
		assert.Equal(t, "inner", string(content))

		_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "sub"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("whole_server_dir", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

		err := Rename(
			resolveEntry(t, confiner, "servers/a"),
			resolveEntry(t, confiner, "servers/renamed"),
		)

		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "renamed", "own.txt"))
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))
	})

	t.Run("missing_source", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

		err := Rename(
			resolveEntry(t, confiner, "servers/a/absent.txt"),
			resolveEntry(t, confiner, "servers/b/moved.txt"),
		)

		require.ErrorIs(t, err, os.ErrNotExist)

		_, err = os.Lstat(filepath.Join(workDir, "servers", "b", "moved.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("missing_destination_directory", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

		err := Rename(
			resolveEntry(t, confiner, "servers/a/own.txt"),
			resolveEntry(t, confiner, "servers/b/absent/moved.txt"),
		)

		require.ErrorIs(t, err, os.ErrNotExist)

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "own.txt"))
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))
	})
}

func TestMoveByCopy(t *testing.T) {
	t.Parallel()

	openRoots := func(t *testing.T) (string, *os.Root, *os.Root) {
		t.Helper()

		workDir := confineTree(t)

		src, err := os.OpenRoot(filepath.Join(workDir, "servers", "a"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = src.Close() })

		dst, err := os.OpenRoot(filepath.Join(workDir, "servers", "b"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = dst.Close() })

		return workDir, src, dst
	}

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		workDir, src, dst := openRoots(t)

		require.NoError(t, moveByCopy(src, "own.txt", dst, "moved.txt"))

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "moved.txt"))
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))

		_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "own.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		workDir, src, dst := openRoots(t)

		require.NoError(t, moveByCopy(src, "sub", dst, "moved"))

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "moved", "inner.txt"))
		require.NoError(t, err)
		assert.Equal(t, "inner", string(content))

		_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "sub"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("existing_destination_is_kept", func(t *testing.T) {
		t.Parallel()

		workDir, src, dst := openRoots(t)

		err := moveByCopy(src, "own.txt", dst, "secret.cfg")

		require.ErrorIs(t, err, os.ErrExist)

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "secret.cfg"))
		require.NoError(t, err)
		assert.Equal(t, "rcon_password secret", string(content))

		content, err = os.ReadFile(filepath.Join(workDir, "servers", "a", "own.txt"))
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))
	})

	t.Run("missing_source_leaves_no_destination", func(t *testing.T) {
		t.Parallel()

		workDir, src, dst := openRoots(t)

		err := moveByCopy(src, "absent.txt", dst, "moved.txt")

		require.ErrorIs(t, err, os.ErrNotExist)

		_, err = os.Lstat(filepath.Join(workDir, "servers", "b", "moved.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}
