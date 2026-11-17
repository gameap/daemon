//go:build linux || darwin

package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantLinks puts into the directory of server "a" the symlinks its tenant can
// create there: some leave the directory, one stays inside it.
func plantLinks(t *testing.T, workDir string) {
	t.Helper()

	serverDir := filepath.Join(workDir, "servers", "a")

	for name, target := range map[string]string{
		"servers-link": "..",
		"node-link":    "../..",
		"file-link":    "../b/secret.cfg",
		"abs-link":     filepath.Join(workDir, "servers", "b"),
		"in-link":      "sub",
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(serverDir, name)))
	}
}

func TestConfiner_SymlinksOutOfTheServerDirAreRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverDirs ServerDirLister
		root       string
		path       string
		wantError  string
	}{
		{
			name:       "neighbour_through_a_link_to_the_parent",
			serverDirs: serverDirs{"servers/a", "servers/b"},
			path:       "servers/a/servers-link/b/secret.cfg",
			wantError:  "path escapes from parent",
		},
		{
			name:       "node_files_through_a_link_to_the_work_dir",
			serverDirs: serverDirs{"servers/a"},
			path:       "servers/a/node-link/steamcmd/steamcmd.sh",
			wantError:  "path escapes from parent",
		},
		{
			name:       "neighbour_file_through_a_file_link",
			serverDirs: serverDirs{"servers/a"},
			path:       "servers/a/file-link",
			wantError:  "path escapes from parent",
		},
		{
			name:       "absolute_link",
			serverDirs: serverDirs{"servers/a"},
			path:       "servers/a/abs-link/secret.cfg",
			wantError:  "path escapes from parent",
		},
		{
			name:      "neighbour_through_a_link_with_a_request_root",
			root:      "servers/a",
			path:      "servers/a/servers-link/b/secret.cfg",
			wantError: "path escapes from parent",
		},
		{
			name:      "node_files_through_a_link_with_a_request_root",
			root:      "servers/a",
			path:      "servers/a/node-link/steamcmd/steamcmd.sh",
			wantError: "path escapes from parent",
		},
		{
			name:       "link_staying_inside_is_followed",
			serverDirs: serverDirs{"servers/a"},
			path:       "servers/a/in-link/inner.txt",
		},
		{
			name: "link_staying_inside_is_followed_with_a_request_root",
			root: "servers/a",
			path: "servers/a/in-link/inner.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			plantLinks(t, workDir)

			confiner := NewConfiner(workDir, WithServerDirs(tt.serverDirs), WithRoot(tt.root))

			got, err := confiner.Resolve(tt.path, TargetEntry)
			require.NoError(t, err)
			t.Cleanup(func() { _ = got.Close() })

			assert.Equal(t, "servers/a", got.Scope)

			content, err := got.Root.ReadFile(got.Rel)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Empty(t, content)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "inner", string(content))
		})
	}
}

// Without a boundary the work directory is the only limit, and a link from one
// server into another stays inside it. This is what the confinement closes.
func TestConfiner_WithoutServerDirsALinkReachesTheNeighbour(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	plantLinks(t, workDir)

	got, err := NewConfiner(workDir).Resolve("servers/a/servers-link/b/secret.cfg", TargetEntry)
	require.NoError(t, err)
	t.Cleanup(func() { _ = got.Close() })

	content, err := got.Root.ReadFile(got.Rel)

	require.NoError(t, err)
	assert.Equal(t, "rcon_password secret", string(content))
}

func TestConfiner_ServerDirBehindASymlink(t *testing.T) {
	t.Parallel()

	t.Run("link_inside_the_work_dir_is_followed_and_confines", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		plantLinks(t, workDir)
		require.NoError(t, os.Symlink("a", filepath.Join(workDir, "servers", "alias")))

		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/alias"}))

		own, err := confiner.Resolve("servers/alias/own.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = own.Close() })

		assert.Equal(t, "servers/alias", own.Scope)

		content, err := own.Root.ReadFile(own.Rel)
		require.NoError(t, err)
		assert.Equal(t, "own", string(content))

		escaping, err := confiner.Resolve("servers/alias/servers-link/b/secret.cfg", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = escaping.Close() })

		_, err = escaping.Root.ReadFile(escaping.Rel)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
	})

	t.Run("link_out_of_the_work_dir_is_refused", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		outside := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outside, "passwd"), []byte("TOPSECRET"), 0o644))
		require.NoError(t, os.Symlink(outside, filepath.Join(workDir, "servers", "outside")))

		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/outside"}))

		got, err := confiner.Resolve("servers/outside/passwd", TargetEntry)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `failed to open confinement directory "servers/outside"`)
		assert.Contains(t, err.Error(), "path escapes from parent")
		assert.Nil(t, got)
	})

	t.Run("dangling_link_leaves_the_enclosing_root", func(t *testing.T) {
		t.Parallel()

		workDir := confineTree(t)
		require.NoError(t, os.Symlink("absent", filepath.Join(workDir, "servers", "dangling")))

		confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/dangling"}))

		got, err := confiner.Resolve("servers/dangling/file.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = got.Close() })

		assert.Equal(t, ".", got.Scope)
		assert.Equal(t, "servers/dangling/file.txt", got.Rel)
	})
}

// The outer tenant owns the directory a nested server lives in, so the nested
// boundary is opened from inside the outer one: a link the outer tenant puts in
// its place cannot carry the request out of the outer directory. That holds
// whether the nested directory is known to the daemon or named by the request.
func TestConfiner_NestedServerDirReplacedByALink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serverDirs ServerDirLister
		root       string
	}{
		{
			name:       "nested_dir_known_to_the_daemon",
			serverDirs: serverDirs{"servers/a", "servers/a/nested"},
		},
		{
			name:       "nested_dir_named_by_the_request",
			serverDirs: serverDirs{"servers/a"},
			root:       "servers/a/nested",
		},
		{
			name:       "nested_dir_both_known_and_named",
			serverDirs: serverDirs{"servers/a", "servers/a/nested"},
			root:       "servers/a/nested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			require.NoError(t, os.RemoveAll(filepath.Join(workDir, "servers", "a", "nested")))
			require.NoError(t, os.Symlink("../b", filepath.Join(workDir, "servers", "a", "nested")))

			confiner := NewConfiner(workDir, WithServerDirs(tt.serverDirs), WithRoot(tt.root))

			got, err := confiner.Resolve("servers/a/nested/secret.cfg", TargetEntry)

			require.Error(t, err)
			assert.Contains(t, err.Error(), `failed to open confinement directory "servers/a/nested"`)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Nil(t, got)
		})
	}
}

// A request root does not lift the boundary of a game server directory inside
// it: a link there still cannot lead into the rest of the root.
func TestConfiner_RequestRootKeepsTheServerDirBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		root string
	}{
		{name: "root_of_the_work_dir", root: "/"},
		{name: "root_enclosing_the_server_dirs", root: "servers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			plantLinks(t, workDir)

			confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}), WithRoot(tt.root))

			got, err := confiner.Resolve("servers/a/servers-link/b/secret.cfg", TargetEntry)
			require.NoError(t, err)
			t.Cleanup(func() { _ = got.Close() })

			assert.Equal(t, "servers/a", got.Scope)

			content, err := got.Root.ReadFile(got.Rel)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Empty(t, content)
		})
	}
}

// A game server directory reached under a name the daemon does not know it by
// is still that directory: the request is confined to it all the same.
func TestConfiner_ServerDirUnderAnotherName(t *testing.T) {
	t.Parallel()

	newTree := func(t *testing.T) string {
		t.Helper()

		workDir := confineTree(t)
		plantLinks(t, workDir)
		require.NoError(t, os.Symlink("a", filepath.Join(workDir, "servers", "alias")))

		return workDir
	}

	known := WithServerDirs(serverDirs{"servers/a", "servers/b"})

	t.Run("path_below_it_is_confined", func(t *testing.T) {
		t.Parallel()

		got, err := NewConfiner(newTree(t), known).Resolve("servers/alias/servers-link/b/secret.cfg", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = got.Close() })

		assert.Equal(t, "servers/alias", got.Scope)
		assert.Equal(t, "servers-link/b/secret.cfg", got.Rel)

		_, err = got.Root.ReadFile(got.Rel)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
	})

	t.Run("own_file_stays_reachable", func(t *testing.T) {
		t.Parallel()

		got, err := NewConfiner(newTree(t), known).Resolve("servers/alias/sub/inner.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = got.Close() })

		content, err := got.Root.ReadFile(got.Rel)
		require.NoError(t, err)
		assert.Equal(t, "inner", string(content))
	})

	t.Run("the_directory_itself_follows_the_target", func(t *testing.T) {
		t.Parallel()

		confiner := NewConfiner(newTree(t), known)

		entry, err := confiner.Resolve("servers/alias", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = entry.Close() })
		assert.Equal(t, ".", entry.Scope)

		tree, err := confiner.Resolve("servers/alias", TargetTree)
		require.NoError(t, err)
		t.Cleanup(func() { _ = tree.Close() })
		assert.Equal(t, "servers/alias", tree.Scope)
		assert.Equal(t, ".", tree.Rel)
	})

	t.Run("it_is_inside_the_request_root_it_names", func(t *testing.T) {
		t.Parallel()

		got, err := NewConfiner(newTree(t), WithRoot("servers/a")).Resolve("servers/alias/own.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = got.Close() })

		assert.Equal(t, "servers/alias", got.Scope)

		_, err = got.Root.Stat("servers-link/b/secret.cfg")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
	})

	t.Run("another_directory_stays_outside_the_request_root", func(t *testing.T) {
		t.Parallel()

		workDir := newTree(t)
		require.NoError(t, os.Symlink("b", filepath.Join(workDir, "servers", "other")))

		got, err := NewConfiner(workDir, WithRoot("servers/a")).Resolve("servers/other/secret.cfg", TargetEntry)

		require.ErrorIs(t, err, ErrPathOutsideConfinement)
		assert.Nil(t, got)
	})

	t.Run("both_names_share_one_root", func(t *testing.T) {
		t.Parallel()

		confiner := NewConfiner(newTree(t), known)

		byName, err := confiner.Resolve("servers/a/own.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = byName.Close() })

		byAlias, err := confiner.Resolve("servers/alias/sub/inner.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = byAlias.Close() })

		assert.True(t, byName.SameRoot(byAlias))
	})
}

// Whether "Servers/a" is the directory "servers/a" depends on the volume, so
// the daemon asks the disk rather than compare the names: both answers have to
// come out right. The test takes whichever volume it runs on.
func TestConfiner_ServerDirSpelledInAnotherCase(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	plantLinks(t, workDir)

	known := WithServerDirs(serverDirs{"servers/a", "servers/b"})

	lower, err := os.Stat(filepath.Join(workDir, "servers"))
	require.NoError(t, err)

	if upper, statErr := os.Stat(filepath.Join(workDir, "SERVERS")); statErr == nil && os.SameFile(lower, upper) {
		got, err := NewConfiner(workDir, known).Resolve("SERVERS/A/servers-link/b/secret.cfg", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = got.Close() })

		assert.Equal(t, "SERVERS/A", got.Scope)

		_, err = got.Root.ReadFile(got.Rel)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")

		rooted, err := NewConfiner(workDir, WithRoot("servers/a")).Resolve("Servers/A/own.txt", TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = rooted.Close() })

		assert.Equal(t, "Servers/A", rooted.Scope)
		assert.True(t, got.SameRoot(rooted))

		return
	}

	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "SERVERS", "A"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "SERVERS", "A", "own.txt"), []byte("another"), 0o644))

	got, err := NewConfiner(workDir, known).Resolve("SERVERS/A/own.txt", TargetEntry)
	require.NoError(t, err)
	t.Cleanup(func() { _ = got.Close() })

	assert.Equal(t, ".", got.Scope, "a directory of its own is not the game server directory")

	rooted, err := NewConfiner(workDir, WithRoot("servers/a")).Resolve("SERVERS/A/own.txt", TargetEntry)

	require.ErrorIs(t, err, ErrPathOutsideConfinement)
	assert.Nil(t, rooted)
}
