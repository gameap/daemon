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

// A symlink standing where a file is copied to is, left alone, a way to write
// somewhere else. With ReplaceSymlinks the link goes and the file takes its
// place; without it the root decides how far the link is followed.
func TestCopyTree_SymlinkInPlaceOfAFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts CopyOptions
		// link returns the target of the symlink standing at dst/game.cfg.
		link        func(outside string) string
		wantError   string
		wantFile    bool
		wantInside  string
		wantOutside string
	}{
		{
			name:        "link_out_of_the_root_is_replaced",
			opts:        CopyOptions{ReplaceSymlinks: true},
			link:        func(outside string) string { return outside },
			wantFile:    true,
			wantInside:  "inside",
			wantOutside: "outside",
		},
		{
			name:        "link_inside_the_root_is_replaced",
			opts:        CopyOptions{ReplaceSymlinks: true},
			link:        func(string) string { return "other.cfg" },
			wantFile:    true,
			wantInside:  "inside",
			wantOutside: "outside",
		},
		{
			name:        "link_to_nowhere_is_replaced",
			opts:        CopyOptions{ReplaceSymlinks: true},
			link:        func(outside string) string { return outside + ".absent" },
			wantFile:    true,
			wantInside:  "inside",
			wantOutside: "outside",
		},
		{
			name:        "link_out_of_the_root_stops_the_copy_by_default",
			link:        func(outside string) string { return outside },
			wantError:   "path escapes from parent",
			wantInside:  "inside",
			wantOutside: "outside",
		},
		{
			name:        "link_inside_the_root_is_written_through_by_default",
			link:        func(string) string { return "other.cfg" },
			wantInside:  "from repository",
			wantOutside: "outside",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := t.TempDir()
			dst := t.TempDir()
			outside := filepath.Join(t.TempDir(), "passwd")

			require.NoError(t, os.WriteFile(filepath.Join(src, "game.cfg"), []byte("from repository"), 0o640))
			require.NoError(t, os.WriteFile(filepath.Join(dst, "other.cfg"), []byte("inside"), 0o600))
			require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o600))
			require.NoError(t, os.Symlink(tt.link(outside), filepath.Join(dst, "game.cfg")))

			err := CopyTree(openTestRoot(t, src), ".", openTestRoot(t, dst), ".", tt.opts)

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			} else {
				require.NoError(t, err)
			}

			inside, err := os.ReadFile(filepath.Join(dst, "other.cfg"))
			require.NoError(t, err)
			assert.Equal(t, tt.wantInside, string(inside))

			outsideContent, err := os.ReadFile(outside)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOutside, string(outsideContent))
			assert.NoFileExists(t, outside+".absent")

			info, err := os.Lstat(filepath.Join(dst, "game.cfg"))
			require.NoError(t, err)

			if !tt.wantFile {
				assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays where it stood")

				return
			}

			require.True(t, info.Mode().IsRegular(), "game.cfg has to be a regular file, it is %s", info.Mode())
			assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())

			content, err := os.ReadFile(filepath.Join(dst, "game.cfg"))
			require.NoError(t, err)
			assert.Equal(t, "from repository", string(content))
		})
	}
}

// A link in the destination that leads back to the file being copied is taken
// away like any other: the source keeps its content and the copy is a file of
// its own.
func TestCopyTree_ReplaceSymlinks_LinkToTheSourceFile(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()

	source := filepath.Join(src, "server.jar")
	require.NoError(t, os.WriteFile(source, []byte("jar"), 0o644))
	require.NoError(t, os.Symlink(source, filepath.Join(dst, "server.jar")))

	err := CopyTree(openTestRoot(t, src), ".", openTestRoot(t, dst), ".", CopyOptions{ReplaceSymlinks: true})
	require.NoError(t, err)

	content, err := os.ReadFile(source)
	require.NoError(t, err)
	assert.Equal(t, "jar", string(content), "the source must not be emptied")

	info, err := os.Lstat(filepath.Join(dst, "server.jar"))
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
}

// A directory of the source that meets a symlink is created through it only as
// far as the root follows: ReplaceSymlinks is about files and takes no tree
// away.
func TestCopyTree_ReplaceSymlinks_LinkInPlaceOfADirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		link      func(outside string) string
		wantError string
		wantFile  string
	}{
		{
			name:      "link_out_of_the_root_stops_the_copy",
			link:      func(outside string) string { return outside },
			wantError: "path escapes from parent",
		},
		{
			name:     "link_inside_the_root_is_merged_through",
			link:     func(string) string { return "real" },
			wantFile: "real/plugins.ini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src := t.TempDir()
			dst := t.TempDir()
			outside := t.TempDir()

			require.NoError(t, os.Mkdir(filepath.Join(src, "addons"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(src, "addons", "plugins.ini"), []byte("plugins"), 0o644))
			require.NoError(t, os.Mkdir(filepath.Join(dst, "real"), 0o755))
			require.NoError(t, os.Symlink(tt.link(outside), filepath.Join(dst, "addons")))

			err := CopyTree(openTestRoot(t, src), ".", openTestRoot(t, dst), ".", CopyOptions{ReplaceSymlinks: true})

			entries, readErr := os.ReadDir(outside)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "nothing may be created outside of the destination")

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)

				return
			}

			require.NoError(t, err)

			content, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(tt.wantFile)))
			require.NoError(t, err)
			assert.Equal(t, "plugins", string(content))
		})
	}
}
