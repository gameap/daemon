package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type serverDirs []string

func (d serverDirs) ServerDirs() []string {
	return d
}

// confineTree lays out a work directory with two game servers and a directory
// that belongs to the node itself.
func confineTree(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()

	for name, content := range map[string]string{
		"servers/a/own.txt":        "own",
		"servers/a/sub/inner.txt":  "inner",
		"servers/a/nested/deep.md": "deep",
		"servers/b/secret.cfg":     "rcon_password secret",
		"servers/ab/other.txt":     "other",
		"steamcmd/steamcmd.sh":     "#!/bin/sh",
	} {
		p := filepath.Join(workDir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}

	return workDir
}

func TestConfiner_Resolve(t *testing.T) {
	t.Parallel()

	known := serverDirs{"servers/a", "servers/b"}

	tests := []struct {
		name       string
		serverDirs ServerDirLister
		root       string
		path       string
		target     Target
		wantScope  string
		wantRel    string
		wantError  string
	}{
		{
			name:       "path_below_a_server_dir_is_confined_to_it",
			serverDirs: known,
			path:       "servers/a/sub/inner.txt",
			wantScope:  "servers/a",
			wantRel:    "sub/inner.txt",
		},
		{
			name:       "absolute_looking_path_is_work_dir_relative",
			serverDirs: known,
			path:       "/servers/a/own.txt",
			wantScope:  "servers/a",
			wantRel:    "own.txt",
		},
		{
			name:       "server_dir_as_entry_is_reached_from_outside",
			serverDirs: known,
			path:       "servers/a",
			target:     TargetEntry,
			wantScope:  ".",
			wantRel:    "servers/a",
		},
		{
			name:       "server_dir_as_tree_confines",
			serverDirs: known,
			path:       "servers/a",
			target:     TargetTree,
			wantScope:  "servers/a",
			wantRel:    ".",
		},
		{
			name:       "path_outside_every_server_dir_stays_in_work_dir",
			serverDirs: known,
			path:       "steamcmd/steamcmd.sh",
			wantScope:  ".",
			wantRel:    "steamcmd/steamcmd.sh",
		},
		{
			name:       "name_sharing_a_prefix_is_not_the_server_dir",
			serverDirs: known,
			path:       "servers/ab/other.txt",
			wantScope:  ".",
			wantRel:    "servers/ab/other.txt",
		},
		{
			name:       "work_dir_itself",
			serverDirs: known,
			path:       "",
			target:     TargetTree,
			wantScope:  ".",
			wantRel:    ".",
		},
		{
			name:       "server_dir_not_on_disk_yet_leaves_the_enclosing_root",
			serverDirs: serverDirs{"servers/new"},
			path:       "servers/new/cfg/server.cfg",
			wantScope:  ".",
			wantRel:    "servers/new/cfg/server.cfg",
		},
		{
			name:       "nested_server_dirs_confine_to_the_innermost",
			serverDirs: serverDirs{"servers/a/nested", "servers/a", "servers"},
			path:       "servers/a/nested/deep.md",
			wantScope:  "servers/a/nested",
			wantRel:    "deep.md",
		},
		{
			name:       "nested_server_dir_missing_keeps_the_outer_one",
			serverDirs: serverDirs{"servers/a", "servers/a/absent"},
			path:       "servers/a/absent/file.txt",
			wantScope:  "servers/a",
			wantRel:    "absent/file.txt",
		},
		{
			name:       "server_dirs_are_normalized",
			serverDirs: serverDirs{"/servers/a/", "servers/a"},
			path:       "servers/a/own.txt",
			wantScope:  "servers/a",
			wantRel:    "own.txt",
		},
		{
			name:       "unusable_server_dirs_are_ignored",
			serverDirs: serverDirs{"", ".", "/", "../outside", "servers/a/../.."},
			path:       "servers/a/own.txt",
			wantScope:  ".",
			wantRel:    "servers/a/own.txt",
		},
		{
			name:      "no_server_dirs_known",
			path:      "servers/a/own.txt",
			wantScope: ".",
			wantRel:   "servers/a/own.txt",
		},
		{
			name:      "request_root_confines",
			root:      "servers/a",
			path:      "servers/a/sub/inner.txt",
			wantScope: "servers/a",
			wantRel:   "sub/inner.txt",
		},
		{
			name:      "request_root_is_normalized",
			root:      "/servers/a/",
			path:      "/servers/a/own.txt",
			wantScope: "servers/a",
			wantRel:   "own.txt",
		},
		{
			name:      "request_root_itself_as_entry_stays_inside",
			root:      "servers/a",
			path:      "servers/a",
			target:    TargetEntry,
			wantScope: "servers/a",
			wantRel:   ".",
		},
		{
			name:       "server_dir_inside_the_request_root_still_confines",
			serverDirs: known,
			root:       "servers",
			path:       "servers/a/own.txt",
			wantScope:  "servers/a",
			wantRel:    "own.txt",
		},
		{
			name:       "request_root_inside_a_server_dir_confines_further",
			serverDirs: known,
			root:       "servers/a/nested",
			path:       "servers/a/nested/deep.md",
			wantScope:  "servers/a/nested",
			wantRel:    "deep.md",
		},
		{
			name:       "request_root_matching_the_server_dir",
			serverDirs: known,
			root:       "servers/a",
			path:       "servers/a/sub/inner.txt",
			wantScope:  "servers/a",
			wantRel:    "sub/inner.txt",
		},
		{
			name:       "request_root_itself_as_entry_of_a_known_server_dir_stays_inside",
			serverDirs: known,
			root:       "servers/a",
			path:       "servers/a",
			target:     TargetEntry,
			wantScope:  "servers/a",
			wantRel:    ".",
		},
		{
			name:      "request_root_of_the_work_dir",
			root:      "/",
			path:      "servers/a/own.txt",
			wantScope: ".",
			wantRel:   "servers/a/own.txt",
		},
		{
			name:       "request_root_of_the_work_dir_keeps_the_server_dirs",
			serverDirs: known,
			root:       "/",
			path:       "servers/a/own.txt",
			wantScope:  "servers/a",
			wantRel:    "own.txt",
		},
		{
			name:       "request_root_not_on_disk_yet_keeps_the_enclosing_server_dir",
			serverDirs: known,
			root:       "servers/a/absent",
			path:       "servers/a/absent/file.txt",
			wantScope:  "servers/a",
			wantRel:    "absent/file.txt",
		},
		{
			name:      "request_root_not_on_disk_yet_leaves_the_work_dir",
			root:      "servers/new",
			path:      "servers/new/cfg/server.cfg",
			wantScope: ".",
			wantRel:   "servers/new/cfg/server.cfg",
		},
		{
			name:       "path_outside_the_request_root",
			serverDirs: known,
			root:       "servers/a",
			path:       "servers/b/secret.cfg",
			wantError:  "path is outside its confinement root",
		},
		{
			name:      "path_above_the_request_root",
			root:      "servers/a",
			path:      "servers",
			wantError: "path is outside its confinement root",
		},
		{
			name:      "name_sharing_a_prefix_is_outside_the_request_root",
			root:      "servers/a",
			path:      "servers/ab/other.txt",
			wantError: "path is outside its confinement root",
		},
		{
			name:      "dot_dot_into_the_neighbour_is_outside_the_request_root",
			root:      "servers/a",
			path:      "servers/a/../b/secret.cfg",
			wantError: "path is outside its confinement root",
		},
		{
			name:       "dot_dot_into_the_neighbour_is_confined_to_the_neighbour",
			serverDirs: known,
			path:       "servers/a/../b/secret.cfg",
			wantScope:  "servers/b",
			wantRel:    "secret.cfg",
		},
		{
			name:      "path_escaping_the_work_dir_lexically",
			root:      "servers/a",
			path:      "servers/a/../../../etc/passwd",
			wantError: "path is outside work directory",
		},
		{
			name:      "request_root_escaping_lexically",
			root:      "../servers",
			path:      "servers/a/own.txt",
			wantError: "path is outside work directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			confiner := NewConfiner(workDir, WithServerDirs(tt.serverDirs), WithRoot(tt.root))

			got, err := confiner.Resolve(tt.path, tt.target)
			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			t.Cleanup(func() { _ = got.Close() })

			assert.Equal(t, tt.wantScope, got.Scope)
			assert.Equal(t, tt.wantRel, got.Rel)

			// The root has to be opened at the directory Scope names: what Rel
			// points at is found through it exactly when it is on disk there.
			_, rootErr := got.Root.Lstat(got.Rel)
			_, diskErr := os.Lstat(filepath.Join(workDir, filepath.FromSlash(got.Scope), filepath.FromSlash(got.Rel)))
			assert.Equal(t, diskErr == nil, rootErr == nil)
		})
	}
}

func TestConfiner_Resolve_RootIsOpenedAtScope(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a"}))

	got, err := confiner.Resolve("servers/a/own.txt", TargetEntry)
	require.NoError(t, err)
	t.Cleanup(func() { _ = got.Close() })

	content, err := got.Root.ReadFile(got.Rel)
	require.NoError(t, err)
	assert.Equal(t, "own", string(content))

	_, err = got.Root.Stat("b/secret.cfg")
	require.Error(t, err, "a neighbouring server is not below the root")

	_, err = got.Root.Stat("../b/secret.cfg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
}

func TestConfiner_Resolve_WorkDirUnavailable(t *testing.T) {
	t.Parallel()

	confiner := NewConfiner(filepath.Join(t.TempDir(), "absent"), WithServerDirs(serverDirs{"servers/a"}))

	got, err := confiner.Resolve("servers/a/own.txt", TargetEntry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work directory unavailable")
	assert.Nil(t, got)
}

func TestConfiner_Resolve_ServerDirIsAFile(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a/own.txt"}))

	got, err := confiner.Resolve("servers/a/own.txt/inside", TargetEntry)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to open confinement directory "servers/a/own.txt"`)
	assert.Nil(t, got)
}

func TestConfined_SameRoot(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

	resolve := func(p string) *Confined {
		c, err := confiner.Resolve(p, TargetEntry)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })

		return c
	}

	own := resolve("servers/a/own.txt")
	inner := resolve("servers/a/sub/inner.txt")
	neighbour := resolve("servers/b/secret.cfg")
	node := resolve("steamcmd/steamcmd.sh")

	assert.True(t, own.SameRoot(inner))
	assert.False(t, own.SameRoot(neighbour))
	assert.False(t, own.SameRoot(node))
	assert.True(t, node.SameRoot(resolve("servers/a")))
}

func TestConfiner_IsServerDir(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)

	stat := func(name string) os.FileInfo {
		info, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(name)))
		require.NoError(t, err)

		return info
	}

	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{
		"servers/a", "/servers/b/", "servers/absent", "servers/a/own.txt", ".", "../outside",
	}))

	assert.True(t, confiner.IsServerDir(stat("servers/a")))
	assert.True(t, confiner.IsServerDir(stat("servers/b")))
	assert.False(t, confiner.IsServerDir(stat("servers/ab")))
	assert.False(t, confiner.IsServerDir(stat("servers")))
	assert.False(t, confiner.IsServerDir(stat("servers/a/sub")))
	assert.False(t, confiner.IsServerDir(stat("servers/a/own.txt")), "a file is no directory to confine to")
	assert.False(t, confiner.IsServerDir(stat(".")), "the work directory is not a boundary of its own")

	assert.False(t, NewConfiner(workDir).IsServerDir(stat("servers/a")))
	assert.False(t, NewConfiner(workDir, WithRoot("servers/a")).IsServerDir(stat("servers/a")))
}

// The game server directories are the ones on disk when the question is asked.
// One that was replaced since the Confiner was made, or since it was last
// asked, is known by its replacement, and the directory that used to stand
// there is a boundary no more.
func TestConfiner_IsServerDir_FollowsTheDirectoryOnDisk(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	serverDir := filepath.Join(workDir, "servers", "b")

	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

	former, err := os.Stat(serverDir)
	require.NoError(t, err)
	require.True(t, confiner.IsServerDir(former))

	// The directory is put aside rather than removed: a removed one could hand
	// its identity on to the directory created next.
	require.NoError(t, os.Rename(serverDir, serverDir+".old"))
	require.NoError(t, os.Mkdir(serverDir, 0o755))

	replacement, err := os.Stat(serverDir)
	require.NoError(t, err)
	require.False(t, os.SameFile(former, replacement))

	assert.True(t, confiner.IsServerDir(replacement), "the directory now standing there is the boundary")
	assert.False(t, confiner.IsServerDir(former), "the directory put aside is not")

	require.NoError(t, os.Remove(serverDir))

	assert.False(t, confiner.IsServerDir(replacement), "a game server directory that is gone confines nothing")
}

func TestConfiner_Resolve_InServerDir(t *testing.T) {
	t.Parallel()

	known := serverDirs{"servers/a", "servers/b"}

	tests := []struct {
		name       string
		serverDirs ServerDirLister
		root       string
		path       string
		target     Target
		want       bool
	}{
		{name: "path_below_a_server_dir", serverDirs: known, path: "servers/a/sub/inner.txt", want: true},
		{name: "server_dir_as_tree", serverDirs: known, path: "servers/a", target: TargetTree, want: true},
		{name: "server_dir_as_entry", serverDirs: known, path: "servers/a"},
		{name: "path_outside_every_server_dir", serverDirs: known, path: "steamcmd/steamcmd.sh"},
		{name: "directory_holding_the_server_dirs", serverDirs: known, path: "servers", target: TargetTree},
		{name: "server_dir_not_on_disk_yet", serverDirs: serverDirs{"servers/new"}, path: "servers/new/server.cfg"},
		{name: "request_root_the_daemon_does_not_know", root: "servers/a", path: "servers/a/own.txt"},
		{
			name:       "request_root_that_is_a_server_dir_as_entry",
			serverDirs: known, root: "servers/a", path: "servers/a",
			want: true,
		},
		{
			name:       "request_root_inside_a_server_dir",
			serverDirs: known, root: "servers/a/nested", path: "servers/a/nested/deep.md",
			want: true,
		},
		{
			name:       "request_root_enclosing_the_server_dirs",
			serverDirs: known, root: "servers", path: "servers/ab/other.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			confiner := NewConfiner(confineTree(t), WithServerDirs(tt.serverDirs), WithRoot(tt.root))

			got, err := confiner.Resolve(tt.path, tt.target)
			require.NoError(t, err)
			t.Cleanup(func() { _ = got.Close() })

			assert.Equal(t, tt.want, got.InServerDir)
		})
	}
}

// A boundary is compared by the name it is found under and then opened by that
// name. The walk moves into the directory it opened only when that is the one
// it compared; a name that has come to lead elsewhere in between is refused.
func TestPlacer_CrossTakesTheDirectoryItCompared(t *testing.T) {
	t.Parallel()

	newPlacer := func(t *testing.T, at string) (*placer, os.FileInfo) {
		t.Helper()

		work, err := os.OpenRoot(confineTree(t))
		require.NoError(t, err)

		p := &placer{root: work, scope: ".", at: at}
		t.Cleanup(func() { _ = p.root.Close() })

		boundary, err := work.Stat("servers/a")
		require.NoError(t, err)

		p.reqRoot = boundary

		return p, boundary
	}

	t.Run("the_directory_compared", func(t *testing.T) {
		t.Parallel()

		p, boundary := newPlacer(t, "servers/a")

		require.NoError(t, p.cross(boundary))

		assert.Equal(t, "servers/a", p.scope)
		assert.Equal(t, ".", p.at)
		assert.True(t, p.inRoot)

		opened, err := p.root.Stat(".")
		require.NoError(t, err)
		assert.True(t, os.SameFile(boundary, opened))
	})

	t.Run("another_directory_under_the_name", func(t *testing.T) {
		t.Parallel()

		p, boundary := newPlacer(t, "servers/b")
		work := p.root

		err := p.cross(boundary)

		require.Error(t, err)
		assert.Contains(t, err.Error(), `confinement directory "servers/b" was replaced while it was being opened`)

		assert.Same(t, work, p.root, "the walk stays where it was")
		assert.Equal(t, "servers/b", p.at)
		assert.False(t, p.inRoot)
	})

	t.Run("a_directory_that_is_no_boundary", func(t *testing.T) {
		t.Parallel()

		p, _ := newPlacer(t, "servers/b")
		work := p.root

		other, err := work.Stat("servers/b")
		require.NoError(t, err)

		require.NoError(t, p.cross(other))

		assert.Same(t, work, p.root)
		assert.Equal(t, "servers/b", p.at)
	})
}
