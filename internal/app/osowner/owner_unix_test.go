//go:build linux || darwin

package osowner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testOwner is an id no account of a test host has. The trees below are given
// to it by number, so no such account is needed.
const testOwner = 54321

type ownership struct {
	uid  int
	gid  int
	mode os.FileMode
}

func ownershipOf(t *testing.T, path string) ownership {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)

	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	return ownership{uid: int(stat.Uid), gid: int(stat.Gid), mode: info.Mode()}
}

// tenantTree lays out a directory a tenant writes to, with links in it that
// lead to a file and a directory the tenant has no claim to.
func tenantTree(t *testing.T) (tree, outsideDir, outsideFile string) {
	t.Helper()

	base := t.TempDir()
	tree = filepath.Join(base, "tree")
	outsideDir = filepath.Join(base, "outside")
	outsideFile = filepath.Join(outsideDir, "shadow")

	require.NoError(t, os.MkdirAll(filepath.Join(tree, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "sub", "file.txt"), []byte("file"), 0o644))
	require.NoError(t, os.Mkdir(outsideDir, 0o700))
	require.NoError(t, os.WriteFile(outsideFile, []byte("root:!:0:0"), 0o600))

	require.NoError(t, os.Symlink(outsideFile, filepath.Join(tree, "link_to_file")))
	require.NoError(t, os.Symlink(outsideDir, filepath.Join(tree, "link_to_dir")))
	require.NoError(t, os.Symlink("../../outside/shadow", filepath.Join(tree, "sub", "relative_link")))

	return tree, outsideDir, outsideFile
}

// The tree of a game server is handed to its user as root, and the tenant
// decides what the links in it point at. A link is given the owner itself;
// what it leads to is not part of the tree.
func TestApplyRecursiveInRoot_DoesNotFollowSymlinks(t *testing.T) {
	if !isRootUser() {
		t.Skip("changing the owner of files requires root")
	}

	tree, outsideDir, outsideFile := tenantTree(t)
	dirBefore := ownershipOf(t, outsideDir)
	fileBefore := ownershipOf(t, outsideFile)

	err := ApplyRecursiveInRoot(openRoot(t, tree), Options{UID: testOwner, GID: testOwner})
	require.NoError(t, err)

	for _, name := range []string{".", "sub", "sub/file.txt", "link_to_file", "link_to_dir", "sub/relative_link"} {
		got := ownershipOf(t, filepath.Join(tree, name))
		assert.Equal(t, testOwner, got.uid, name)
		assert.Equal(t, testOwner, got.gid, name)
	}

	assert.Equal(t, dirBefore, ownershipOf(t, outsideDir), "the directory a link leads to is left as it was")
	assert.Equal(t, fileBefore, ownershipOf(t, outsideFile), "the file a link leads to is left as it was")
}

// The same for the tree shared with the group of the game servers: neither the
// group nor the mode of what a link leads to is changed.
func TestApplyGroupSharedRecursiveInRoot_DoesNotFollowSymlinks(t *testing.T) {
	if !isRootUser() {
		t.Skip("changing the group and the mode of files requires root")
	}

	tree, outsideDir, outsideFile := tenantTree(t)
	dirBefore := ownershipOf(t, outsideDir)
	fileBefore := ownershipOf(t, outsideFile)

	err := ApplyGroupSharedRecursiveInRoot(openRoot(t, tree), Options{GID: testOwner})
	require.NoError(t, err)

	for _, name := range []string{".", "sub"} {
		got := ownershipOf(t, filepath.Join(tree, name))
		assert.Equal(t, testOwner, got.gid, name)
		assert.NotZero(t, got.mode&os.ModeSetgid, "%s has to carry the setgid bit", name)
		assert.Equal(t, os.FileMode(0o070), got.mode.Perm()&0o070, "%s has to be group rwx", name)
	}

	file := ownershipOf(t, filepath.Join(tree, "sub", "file.txt"))
	assert.Equal(t, testOwner, file.gid)
	assert.Equal(t, os.FileMode(0o664), file.mode.Perm())

	for _, name := range []string{"link_to_file", "link_to_dir", "sub/relative_link"} {
		assert.Equal(t, testOwner, ownershipOf(t, filepath.Join(tree, name)).gid, name)
	}

	assert.Equal(t, dirBefore, ownershipOf(t, outsideDir), "the directory a link leads to is left as it was")
	assert.Equal(t, fileBefore, ownershipOf(t, outsideFile), "the file a link leads to is left as it was")
}

// The root is where the walk starts and where it stays: a tree opened at one
// directory is not left through a link, whatever the walk meets.
func TestApplyRecursiveInRoot_StaysInsideTheRoot(t *testing.T) {
	if !isRootUser() {
		t.Skip("changing the owner of files requires root")
	}

	base := t.TempDir()
	tree := filepath.Join(base, "tree")
	neighbour := filepath.Join(base, "neighbour")

	require.NoError(t, os.Mkdir(tree, 0o755))
	require.NoError(t, os.Mkdir(neighbour, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(neighbour, "secret.cfg"), []byte("rcon"), 0o600))
	require.NoError(t, os.Symlink("../neighbour", filepath.Join(tree, "neighbour")))

	before := ownershipOf(t, filepath.Join(neighbour, "secret.cfg"))

	err := ApplyRecursiveInRoot(openRoot(t, tree), Options{UID: testOwner, GID: testOwner})
	require.NoError(t, err)

	assert.Equal(t, before, ownershipOf(t, filepath.Join(neighbour, "secret.cfg")))
	assert.Zero(t, ownershipOf(t, neighbour).uid)
}
