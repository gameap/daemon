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

func openTestRoot(t *testing.T, dir string) *os.Root {
	t.Helper()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	return root
}

func TestCopyTree_DirectoryIntoItself(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// srcRoot and dstRoot are relative to the work directory.
		srcRoot string
		src     string
		dstRoot string
		dst     string
		// created is what a copy would have made, relative to the work directory.
		created string
	}{
		{name: "onto_itself", srcRoot: ".", src: "servers/a", dstRoot: ".", dst: "servers/a"},
		{name: "into_a_new_child", srcRoot: ".", src: "servers/a", dstRoot: ".", dst: "servers/a/clone", created: "servers/a/clone"},
		{name: "into_an_existing_child", srcRoot: ".", src: "servers/a", dstRoot: ".", dst: "servers/a/sub"},
		{
			name:    "into_a_new_descendant",
			srcRoot: ".", src: "servers", dstRoot: ".", dst: "servers/a/backups/today/clone",
			created: "servers/a/backups",
		},
		{name: "root_into_its_own_child", srcRoot: "servers/a", src: ".", dstRoot: "servers/a", dst: "clone", created: "servers/a/clone"},
		{
			name:    "destination_named_through_a_root_inside_the_source",
			srcRoot: ".", src: "servers", dstRoot: "servers/a", dst: "clone",
			created: "servers/a/clone",
		},
		{
			name:    "destination_is_a_root_inside_the_source",
			srcRoot: ".", src: "servers", dstRoot: "servers/a", dst: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			before := treeFiles(t, workDir)

			srcRoot := openTestRoot(t, filepath.Join(workDir, filepath.FromSlash(tt.srcRoot)))
			dstRoot := openTestRoot(t, filepath.Join(workDir, filepath.FromSlash(tt.dstRoot)))

			err := CopyTree(srcRoot, tt.src, dstRoot, tt.dst, CopyOptions{})

			require.ErrorIs(t, err, ErrCopyIntoItself)
			assert.Equal(t, before, treeFiles(t, workDir), "a refused copy changes no file")

			if tt.created != "" {
				_, statErr := os.Lstat(filepath.Join(workDir, filepath.FromSlash(tt.created)))
				assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused copy leaves no destination behind")
			}
		})
	}
}

func TestCopyTree_NextToTheSourceIsNotIntoIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		dst  string
		want string
	}{
		{name: "sibling", src: "servers/a", dst: "servers/clone", want: "servers/clone/sub/inner.txt"},
		{name: "name_sharing_a_prefix", src: "servers/a", dst: "servers/a-clone", want: "servers/a-clone/sub/inner.txt"},
		{name: "into_another_server", src: "servers/a", dst: "servers/b/from-a", want: "servers/b/from-a/sub/inner.txt"},
		{name: "child_into_its_parent", src: "servers/a/sub", dst: "servers/a", want: "servers/a/inner.txt"},
		{name: "child_next_to_itself", src: "servers/a/sub", dst: "servers/a/sub2", want: "servers/a/sub2/inner.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			root := openTestRoot(t, workDir)

			require.NoError(t, CopyTree(root, tt.src, root, tt.dst, CopyOptions{}))

			content, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(tt.want)))
			require.NoError(t, err)
			assert.Equal(t, "inner", string(content))
		})
	}
}

func TestCopyTree_FileOntoItself(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	root := openTestRoot(t, workDir)
	serverRoot := openTestRoot(t, filepath.Join(workDir, "servers", "a"))

	err := CopyTree(root, "servers/a/own.txt", root, "servers/a/own.txt", CopyOptions{})
	require.ErrorIs(t, err, ErrCopySameFile)

	err = CopyTree(root, "servers/a/own.txt", serverRoot, "own.txt", CopyOptions{})
	require.ErrorIs(t, err, ErrCopySameFile)

	content, err := os.ReadFile(filepath.Join(workDir, "servers", "a", "own.txt"))
	require.NoError(t, err)
	assert.Equal(t, "own", string(content), "the source must not be emptied")

	require.NoError(t, CopyTree(root, "servers/a/own.txt", root, "servers/a/own-copy.txt", CopyOptions{}))
}

// A second name of the source is the source: writing through it would empty
// the file before a byte of it is read.
func TestCopyTree_FileOntoItsHardLink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	own := filepath.Join(workDir, "servers", "a", "own.txt")

	if err := os.Link(own, filepath.Join(workDir, "servers", "a", "own-hard.txt")); err != nil {
		t.Skipf("cannot create a hard link: %v", err)
	}

	root := openTestRoot(t, workDir)

	err := CopyTree(root, "servers/a/own.txt", root, "servers/a/own-hard.txt", CopyOptions{})
	require.ErrorIs(t, err, ErrCopySameFile)

	content, err := os.ReadFile(own)
	require.NoError(t, err)
	assert.Equal(t, "own", string(content), "the source must not be emptied")
}

// The destination is emptied once it is known to be another file, so what was
// in it does not outlast a shorter copy.
func TestCopyTree_FileOverALongerOne(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	root := openTestRoot(t, workDir)

	require.NoError(t, CopyTree(root, "servers/a/own.txt", root, "servers/b/secret.cfg", CopyOptions{}))

	content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "secret.cfg"))
	require.NoError(t, err)
	assert.Equal(t, "own", string(content))
}

// treeFiles reads every regular file below dir, keyed by its slash-separated
// path, and lists the directories, so that two snapshots compare as a whole.
func treeFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	require.NoError(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}

		switch {
		case d.IsDir():
			files[filepath.ToSlash(rel)+"/"] = ""
		case d.Type().IsRegular():
			content, err := os.ReadFile(p)
			if err != nil {
				return err
			}

			files[filepath.ToSlash(rel)] = string(content)
		}

		return nil
	}))

	return files
}
