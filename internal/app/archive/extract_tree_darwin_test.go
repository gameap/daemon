//go:build darwin

package archive

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// A run that cannot give a directory its mode in the end fails, and is taken
// back like any other: what it created goes, also out of a directory that has
// taken a mode closed to its owner by then.
//
// Nothing about the tree keeps an owner from changing the mode of its own
// directory but a flag of the file system. The flag that does is one any owner
// may set on macOS, and one only a privileged process may set on Linux.
func TestExtractTree_RunThatCannotSettleTheModesIsTakenBack(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)
	keepRemovable(t, dir)

	kept := filepath.Join(dir, "kept")
	require.NoError(t, os.Mkdir(kept, 0o755))
	t.Cleanup(func() { _ = unix.Chflags(kept, 0) })

	archive := buildTarGz(t, []tarEntry{
		{name: "kept/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "new/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "new/models/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "new/models/player.mdl", body: "mdl", mode: 0o444},
	})

	// Once its entry is through, the directory that was there cannot take its
	// mode any more. The other ones take theirs before the run finds out.
	progress := func(_, _ int64, name string) {
		if name == "kept/" {
			require.NoError(t, unix.Chflags(kept, unix.UF_IMMUTABLE))
		}
	}

	res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{
		Progress: progress,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to settle the directory modes of archive "game.tar.gz"`)
	assert.Contains(t, err.Error(), `failed to chmod directory "kept"`)
	assert.Nil(t, res)

	assert.Equal(t, []string{"kept"}, entryNames(t, dir), "nothing new is left in the destination")
}
