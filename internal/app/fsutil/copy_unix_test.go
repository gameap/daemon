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

// The destination reaches into the source through a link, so no directory the
// destination is named by is the source. The copy is stopped where it finds
// its own destination, and what it made until then is removed.
func TestCopyTree_DirectoryIntoItselfThroughALink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	require.NoError(t, os.Symlink("servers/a/nested", filepath.Join(workDir, "shortcut")))

	before := treeFiles(t, workDir)
	root := openTestRoot(t, workDir)

	err := CopyTree(root, "servers", root, "shortcut/clone", CopyOptions{})

	require.ErrorIs(t, err, ErrCopyIntoItself)
	assert.Equal(t, before, treeFiles(t, workDir))

	_, statErr := os.Lstat(filepath.Join(workDir, "servers", "a", "nested", "clone"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCopyTree_FileOntoItselfThroughALink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	require.NoError(t, os.Symlink("own.txt", filepath.Join(workDir, "servers", "a", "own-link.txt")))

	root := openTestRoot(t, workDir)

	err := CopyTree(root, "servers/a/own.txt", root, "servers/a/own-link.txt", CopyOptions{})
	require.ErrorIs(t, err, ErrCopySameFile)

	content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "own.txt"))
	require.NoError(t, err)
	assert.Equal(t, "own", string(content))
}

// A link that is followed is read as the file it leads to. When that file is
// the destination, the copy has nothing to read once the destination is
// emptied: the source is told from the destination as it is opened, not by the
// name it was asked for.
func TestCopyTree_FileOntoItselfThroughAFollowedLink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	require.NoError(t, os.Symlink("own.txt", filepath.Join(workDir, "servers", "a", "own-link.txt")))

	root := openTestRoot(t, workDir)

	err := CopyTree(root, "servers/a/own-link.txt", root, "servers/a/own.txt", CopyOptions{Symlink: SymlinkDeep})
	require.ErrorIs(t, err, ErrCopySameFile)

	content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "own.txt"))
	require.NoError(t, err)
	assert.Equal(t, "own", string(content), "the source must not be emptied")
}

// A link standing in the destination tree leads the write back to the source
// file, whatever the copy does with links it finds in the source.
func TestCopyTree_FileOntoItselfThroughALinkInTheDestination(t *testing.T) {
	t.Parallel()

	for name, opts := range map[string]CopyOptions{
		"shallow": {},
		"deep":    {Symlink: SymlinkDeep},
		"skip":    {Symlink: SymlinkSkip},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			mirror := filepath.Join(workDir, "servers", "a", "mirror")

			require.NoError(t, os.Mkdir(mirror, 0o755))
			require.NoError(t, os.Symlink("../sub/inner.txt", filepath.Join(mirror, "inner.txt")))

			root := openTestRoot(t, workDir)

			err := CopyTree(root, "servers/a/sub", root, "servers/a/mirror", opts)
			require.ErrorIs(t, err, ErrCopySameFile)

			content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "sub", "inner.txt"))
			require.NoError(t, err)
			assert.Equal(t, "inner", string(content), "the source must not be emptied")
		})
	}
}

// A followed link to the directory the copy is written to makes every file in
// it its own destination.
func TestCopyTree_DirectoryOntoItselfThroughAFollowedLink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	require.NoError(t, os.Symlink("a", filepath.Join(workDir, "servers", "alias")))

	before := treeFiles(t, workDir)
	root := openTestRoot(t, workDir)

	err := CopyTree(root, "servers/alias", root, "servers/a", CopyOptions{Symlink: SymlinkDeep})

	require.ErrorIs(t, err, ErrCopySameFile)
	assert.Equal(t, before, treeFiles(t, workDir), "no file may be emptied")
}
