//go:build linux || darwin

package fsutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopy_SymlinkShallowPreservesLink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "target.txt"), []byte("T"), 0o644))
	require.NoError(t, os.Symlink("target.txt", filepath.Join(src, "link")))

	require.NoError(t, Copy(src, dst, CopyOptions{Symlink: SymlinkShallow}))

	info, err := os.Lstat(filepath.Join(dst, "link"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "link must be copied as a symlink, not dereferenced")

	target, err := os.Readlink(filepath.Join(dst, "link"))
	require.NoError(t, err)
	assert.Equal(t, "target.txt", target)
}

func TestCopy_SymlinkSkip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "f.txt"), []byte("F"), 0o644))
	require.NoError(t, os.Symlink("f.txt", filepath.Join(src, "link")))

	require.NoError(t, Copy(src, dst, CopyOptions{Symlink: SymlinkSkip}))

	_, err := os.Lstat(filepath.Join(dst, "link"))
	assert.ErrorIs(t, err, os.ErrNotExist, "skipped symlink must not be created")

	_, err = os.Stat(filepath.Join(dst, "f.txt"))
	require.NoError(t, err, "regular files must still be copied")
}

func TestCopy_SkipsNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	require.NoError(t, os.MkdirAll(src, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "real.txt"), []byte("R"), 0o644))

	if err := syscall.Mkfifo(filepath.Join(src, "pipe"), 0o644); err != nil {
		t.Skipf("cannot create fifo: %v", err)
	}

	require.NoError(t, Copy(src, dst, CopyOptions{}), "a fifo in the tree must not fail the copy")

	got, err := os.ReadFile(filepath.Join(dst, "real.txt"))
	require.NoError(t, err)
	assert.Equal(t, "R", string(got))

	_, err = os.Lstat(filepath.Join(dst, "pipe"))
	assert.ErrorIs(t, err, os.ErrNotExist, "the fifo must be skipped, not copied")
}
