//go:build linux || darwin

package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openDirModes are the ways a caller names the boundaries: not at all, with
// the directory it opens among the game servers the daemon knows, and with
// game servers known of which the directory is none.
func openDirModes(dir string) map[string][]ConfineOption {
	return map[string][]ConfineOption{
		"no_server_dirs":          nil,
		"directory_is_known":      {WithServerDirs(serverDirs{dir, "servers/b"})},
		"other_directories_known": {WithServerDirs(serverDirs{"servers/b"})},
	}
}

func TestOpenDir_OpensTheDirectory(t *testing.T) {
	t.Parallel()

	for mode, opts := range openDirModes("servers/a") {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)

			root, err := OpenDir(workDir, "servers/a", 0o700, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })

			content, err := root.ReadFile("own.txt")
			require.NoError(t, err)
			assert.Equal(t, "own", string(content))

			info, err := os.Stat(filepath.Join(workDir, "servers", "a"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "an existing directory keeps its mode")
		})
	}
}

func TestOpenDir_CreatesAMissingDirectory(t *testing.T) {
	t.Parallel()

	for mode, opts := range openDirModes("fresh/deep/server") {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)

			root, err := OpenDir(workDir, "fresh/deep/server", 0o750, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })

			require.NoError(t, root.WriteFile("installed.txt", []byte("installed"), 0o644))

			content, err := os.ReadFile(filepath.Join(workDir, "fresh", "deep", "server", "installed.txt"))
			require.NoError(t, err)
			assert.Equal(t, "installed", string(content))

			dir, err := os.Stat(filepath.Join(workDir, "fresh", "deep", "server"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o750), dir.Mode().Perm())

			for _, parent := range []string{"fresh", "fresh/deep"} {
				info, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(parent)))
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "%s has to stay passable", parent)
			}
		})
	}
}

// The tenant of a game server owns its directory, and where the work directory
// belongs to the user the servers run as, also the name the directory is known
// by. A symlink put in its place must not carry whatever the daemon does next
// out of the work directory.
func TestOpenDir_SymlinkInPlaceOfTheDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// link returns the target of the symlink put in place of servers/a.
		link      func(t *testing.T, workDir, outside string) string
		wantError string
		wantFile  string
	}{
		{
			name: "absolute_link_out_of_the_work_dir_is_refused",
			link: func(_ *testing.T, _, outside string) string {
				return outside
			},
			wantError: "path escapes from parent",
		},
		{
			name: "relative_link_out_of_the_work_dir_is_refused",
			link: func(t *testing.T, workDir, outside string) string {
				t.Helper()

				rel, err := filepath.Rel(filepath.Join(workDir, "servers"), outside)
				require.NoError(t, err)

				return rel
			},
			wantError: "path escapes from parent",
		},
		{
			name: "link_to_the_parent_of_the_work_dir_is_refused",
			link: func(_ *testing.T, _, _ string) string {
				return "../.."
			},
			wantError: "path escapes from parent",
		},
		{
			name: "dangling_link_is_refused",
			link: func(_ *testing.T, _, _ string) string {
				return "absent"
			},
			wantError: "no such file or directory",
		},
		{
			name: "link_to_a_file_is_refused",
			link: func(_ *testing.T, _, _ string) string {
				return "b/secret.cfg"
			},
			wantError: "not a directory",
		},
		{
			name: "link_to_a_directory_inside_the_work_dir_is_followed",
			link: func(_ *testing.T, _, _ string) string {
				return "ab"
			},
			wantFile: "other.txt",
		},
	}

	for _, tt := range tests {
		for mode, opts := range openDirModes("servers/a") {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				t.Parallel()

				workDir := confineTree(t)
				outside := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(outside, "passwd"), []byte("root:x:0:0"), 0o600))

				serverDir := filepath.Join(workDir, "servers", "a")
				require.NoError(t, os.RemoveAll(serverDir))
				require.NoError(t, os.Symlink(tt.link(t, workDir, outside), serverDir))

				root, err := OpenDir(workDir, "servers/a", 0o755, opts...)

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Nil(t, root)

					entries, err := os.ReadDir(outside)
					require.NoError(t, err)
					require.Len(t, entries, 1, "nothing is created where the link leads")
					assert.NoDirExists(t, filepath.Join(workDir, "servers", "absent"))

					return
				}

				require.NoError(t, err)
				t.Cleanup(func() { _ = root.Close() })

				_, err = root.Stat(tt.wantFile)
				require.NoError(t, err)
			})
		}
	}
}

func TestOpenDir_SymlinkOnTheWayToAMissingDirectory(t *testing.T) {
	t.Parallel()

	for mode, opts := range openDirModes("linked/server") {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			outside := t.TempDir()
			require.NoError(t, os.Symlink(outside, filepath.Join(workDir, "linked")))

			root, err := OpenDir(workDir, "linked/server", 0o755, opts...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Nil(t, root)
			assert.NoDirExists(t, filepath.Join(outside, "server"))
		})
	}
}

func TestOpenDir_FileInPlaceOfTheDirectory(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)

	root, err := OpenDir(workDir, "servers/a/own.txt", 0o755)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
	assert.Nil(t, root)
}

func TestOpenDir_PathOutOfTheWorkDir(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)

	root, err := OpenDir(workDir, "../outside", 0o755)

	require.ErrorIs(t, err, ErrPathOutsideRoot)
	assert.Nil(t, root)
}

// The directory of a game server nested in another one stands in a directory
// the outer tenant owns. It is opened from inside the outer directory, so a
// link the outer tenant puts in its place cannot lead to a neighbour.
func TestOpenDir_NestedServerDirReplacedByALink(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	nested := filepath.Join(workDir, "servers", "a", "nested")
	require.NoError(t, os.RemoveAll(nested))
	require.NoError(t, os.Symlink("../b", nested))

	root, err := OpenDir(workDir, "servers/a/nested", 0o755, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
	assert.Nil(t, root)
}

func TestOpenExistingDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dir       string
		prepare   func(t *testing.T, workDir, outside string)
		wantFile  string
		wantError string
	}{
		{
			name:     "directory_is_opened",
			dir:      "steamcmd",
			wantFile: "steamcmd.sh",
		},
		{
			name:     "work_dir_itself_is_opened",
			dir:      ".",
			wantFile: "steamcmd/steamcmd.sh",
		},
		{
			name:      "missing_directory_is_not_created",
			dir:       "absent",
			wantError: "no such file or directory",
		},
		{
			name: "link_out_of_the_work_dir_is_refused",
			dir:  "steamcmd",
			prepare: func(t *testing.T, workDir, outside string) {
				t.Helper()

				require.NoError(t, os.RemoveAll(filepath.Join(workDir, "steamcmd")))
				require.NoError(t, os.Symlink(outside, filepath.Join(workDir, "steamcmd")))
			},
			wantError: "path escapes from parent",
		},
	}

	for _, tt := range tests {
		for mode, opts := range openDirModes("servers/a") {
			t.Run(tt.name+"/"+mode, func(t *testing.T) {
				t.Parallel()

				workDir := confineTree(t)
				outside := t.TempDir()

				if tt.prepare != nil {
					tt.prepare(t, workDir, outside)
				}

				root, err := OpenExistingDir(workDir, tt.dir, opts...)

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Nil(t, root)
					assert.NoDirExists(t, filepath.Join(workDir, "absent"))

					return
				}

				require.NoError(t, err)
				t.Cleanup(func() { _ = root.Close() })

				_, err = root.Stat(tt.wantFile)
				require.NoError(t, err)
			})
		}
	}
}
