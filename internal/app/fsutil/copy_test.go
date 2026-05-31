package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopy_File(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "out", "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o640))

	require.NoError(t, Copy(src, dst, CopyOptions{}))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "source permissions must be preserved")
}

func TestCopy_FileAddPermission(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	require.NoError(t, os.WriteFile(src, []byte("x"), 0o600))

	require.NoError(t, Copy(src, dst, CopyOptions{AddPermission: 0o011}))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o611), info.Mode().Perm(), "AddPermission must be OR'd into the mode")
}

func TestCopy_DirRecursiveMergeAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	require.NoError(t, os.MkdirAll(filepath.Join(src, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "nested", "b.txt"), []byte("B"), 0o644))

	// Pre-existing destination with a stale file that must be overwritten and
	// an unrelated file that must be preserved (merge semantics).
	require.NoError(t, os.MkdirAll(dst, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "a.txt"), []byte("OLD"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "keep.txt"), []byte("KEEP"), 0o644))

	require.NoError(t, Copy(src, dst, CopyOptions{}))

	a, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "A", string(a), "existing file must be overwritten")

	b, err := os.ReadFile(filepath.Join(dst, "nested", "b.txt"))
	require.NoError(t, err)
	assert.Equal(t, "B", string(b))

	keep, err := os.ReadFile(filepath.Join(dst, "keep.txt"))
	require.NoError(t, err)
	assert.Equal(t, "KEEP", string(keep), "unrelated destination file must be preserved (merge)")
}

func TestCopyTree_CrossRoot(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("data"), 0o644))

	srcRoot, err := os.OpenRoot(srcDir)
	require.NoError(t, err)
	defer srcRoot.Close()
	dstRoot, err := os.OpenRoot(dstDir)
	require.NoError(t, err)
	defer dstRoot.Close()

	require.NoError(t, CopyTree(srcRoot, ".", dstRoot, ".", CopyOptions{}))

	got, err := os.ReadFile(filepath.Join(dstDir, "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))
}

func TestCopyInRoot_SameRoot(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "f.txt"), []byte("z"), 0o644))

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	defer root.Close()

	require.NoError(t, CopyInRoot(root, "a", "b", CopyOptions{}))

	got, err := os.ReadFile(filepath.Join(dir, "b", "f.txt"))
	require.NoError(t, err)
	assert.Equal(t, "z", string(got))
}
