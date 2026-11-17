//go:build linux || darwin

package archive

import (
	"archive/zip"
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gameap/daemon/internal/app/fsutil"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type knownServerDirs []string

func (d knownServerDirs) ServerDirs() []string {
	return d
}

// tenantWorkDir lays out a node with two game servers; the directory of server
// "a" holds the symlinks its tenant can create from inside the game process.
func tenantWorkDir(t *testing.T) string {
	t.Helper()

	workDir := t.TempDir()

	writeTree(t, workDir, map[string]string{
		"servers/a/own.txt":            "own",
		"servers/a/cstrike/server.cfg": "hostname a",
		"servers/b/victim.txt":         "victim",
		"servers/b/cstrike/server.cfg": "rcon_password secret",
		"steamcmd/steamcmd.sh":         "#!/bin/sh",
	})

	for name, target := range map[string]string{
		"servers-link": "..",
		"node-link":    "../..",
		"file-link":    "../b/cstrike/server.cfg",
		"in-link":      "cstrike",
	} {
		require.NoError(t, os.Symlink(target, filepath.Join(workDir, "servers", "a", name)))
	}

	return workDir
}

// confinementModes are the two ways a request ends up confined to server "a":
// the daemon knows the game server directories, or the panel names the root.
func confinementModes() map[string][]fsutil.ConfineOption {
	return map[string][]fsutil.ConfineOption{
		"known_server_dirs": {fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})},
		"request_root":      {fsutil.WithRoot("servers/a")},
	}
}

func zipEntryNames(t *testing.T, path string) []string {
	t.Helper()

	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()

	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}

	return names
}

// zipContents reads the body of every regular entry of a zip archive.
func zipContents(t *testing.T, path string) map[string]string {
	t.Helper()

	r, err := zip.OpenReader(path)
	require.NoError(t, err)
	defer r.Close()

	contents := map[string]string{}

	for _, f := range r.File {
		if !f.Mode().IsRegular() {
			continue
		}

		rc, err := f.Open()
		require.NoError(t, err)

		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		contents[f.Name] = string(body)
	}

	return contents
}

func TestCreate_Confinement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		params    *pb.CreateArchiveParams
		wantError string
		// wantEntries is checked when the archive is expected to be created.
		wantEntries []string
	}{
		{
			name: "own_tree_keeps_links_as_links",
			params: &pb.CreateArchiveParams{
				ArchivePath: "servers/a/backup.zip",
				BasePath:    "servers/a",
				Sources:     []string{"."},
			},
			wantEntries: []string{
				"own.txt", "cstrike/", "cstrike/server.cfg", "servers-link", "node-link", "file-link", "in-link",
			},
		},
		{
			name: "own_tree_following_a_link_that_stays_inside",
			params: &pb.CreateArchiveParams{
				ArchivePath:    "servers/a/backup.zip",
				BasePath:       "servers/a",
				Sources:        []string{"in-link"},
				FollowSymlinks: true,
			},
			wantEntries: []string{"in-link/", "in-link/server.cfg"},
		},
		{
			name: "following_a_link_to_the_parent",
			params: &pb.CreateArchiveParams{
				ArchivePath:    "servers/a/backup.zip",
				BasePath:       "servers/a",
				Sources:        []string{"servers-link"},
				FollowSymlinks: true,
			},
			wantError: "path escapes from parent",
		},
		{
			name: "following_a_link_to_a_neighbour_file",
			params: &pb.CreateArchiveParams{
				ArchivePath:    "servers/a/backup.zip",
				BasePath:       "servers/a",
				Sources:        []string{"file-link"},
				FollowSymlinks: true,
			},
			wantError: "path escapes from parent",
		},
		{
			name: "whole_tree_following_links",
			params: &pb.CreateArchiveParams{
				ArchivePath:    "servers/a/backup.zip",
				BasePath:       "servers/a",
				Sources:        []string{"."},
				FollowSymlinks: true,
			},
			wantError: "path escapes from parent",
		},
		{
			name: "source_named_through_a_link",
			params: &pb.CreateArchiveParams{
				ArchivePath: "servers/a/backup.zip",
				BasePath:    "servers/a",
				Sources:     []string{"servers-link/b/cstrike/server.cfg"},
			},
			wantError: "path escapes from parent",
		},
		{
			name: "base_path_named_through_a_link",
			params: &pb.CreateArchiveParams{
				ArchivePath: "servers/a/backup.zip",
				BasePath:    "servers/a/servers-link/b",
				Sources:     []string{"."},
			},
			wantError: "path escapes from parent",
		},
		{
			name: "archive_written_through_a_link",
			params: &pb.CreateArchiveParams{
				ArchivePath: "servers/a/servers-link/b/planted.zip",
				BasePath:    "servers/a",
				Sources:     []string{"own.txt"},
			},
			wantError: "path escapes from parent",
		},
		{
			name: "archive_written_over_a_node_file_through_a_link",
			params: &pb.CreateArchiveParams{
				ArchivePath: "servers/a/node-link/steamcmd/steamcmd.sh",
				Format:      pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
				BasePath:    "servers/a",
				Sources:     []string{"own.txt"},
				Overwrite:   true,
			},
			wantError: "path escapes from parent",
		},
	}

	for modeName, opts := range confinementModes() {
		for _, tt := range tests {
			t.Run(modeName+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				workDir := tenantWorkDir(t)
				neighbour := readTree(t, filepath.Join(workDir, "servers", "b"))
				node := readTree(t, filepath.Join(workDir, "steamcmd"))

				res, err := Create(context.Background(), workDir, tt.params, nil, opts...)

				assert.Equal(t, neighbour, readTree(t, filepath.Join(workDir, "servers", "b")))
				assert.Equal(t, node, readTree(t, filepath.Join(workDir, "steamcmd")))

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Nil(t, res)

					_, statErr := os.Lstat(filepath.Join(workDir, "servers", "a", "backup.zip"))
					assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused operation leaves no archive behind")

					return
				}

				require.NoError(t, err)
				assert.ElementsMatch(
					t, tt.wantEntries, zipEntryNames(t, filepath.Join(workDir, "servers", "a", "backup.zip")),
				)
			})
		}
	}
}

func TestExtract_Confinement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		entries   []zipEntry
		params    *pb.ExtractArchiveParams
		wantError string
		// wantFiles are the regular files expected below the directory of
		// server "a" once the archive is unpacked.
		wantFiles map[string]string
	}{
		{
			name:    "into_own_directory",
			entries: []zipEntry{{name: "addons/plugin.cfg", mode: 0o644, body: "cfg"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:       "servers/a/in.zip",
				Destination:       "servers/a/unpacked",
				CreateDestination: true,
			},
			wantFiles: map[string]string{"unpacked/addons/plugin.cfg": "cfg"},
		},
		{
			name:    "into_own_directory_behind_a_link_staying_inside",
			entries: []zipEntry{{name: "motd.txt", mode: 0o644, body: "motd"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath: "servers/a/in.zip",
				Destination: "servers/a/in-link",
			},
			wantFiles: map[string]string{"cstrike/motd.txt": "motd"},
		},
		{
			name:    "destination_named_through_a_link",
			entries: []zipEntry{{name: "planted.txt", mode: 0o644, body: "x"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:       "servers/a/in.zip",
				Destination:       "servers/a/servers-link/b/unpacked",
				CreateDestination: true,
			},
			wantError: "path escapes from parent",
		},
		{
			name:    "destination_is_a_link_to_the_node",
			entries: []zipEntry{{name: "steamcmd/steamcmd.sh", mode: 0o755, body: "id > /tmp/pwned"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:    "servers/a/in.zip",
				Destination:    "servers/a/node-link",
				ConflictPolicy: pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
			},
			wantError: "path escapes from parent",
		},
		{
			name:    "entry_unpacked_through_a_link_standing_in_the_destination",
			entries: []zipEntry{{name: "servers-link/b/cstrike/server.cfg", mode: 0o644, body: "rcon_password owned"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:    "servers/a/in.zip",
				Destination:    "servers/a",
				ConflictPolicy: pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
			},
			wantError: "path escapes from parent",
		},
		{
			name:    "entry_unpacked_over_a_node_file_through_a_link",
			entries: []zipEntry{{name: "node-link/steamcmd/steamcmd.sh", mode: 0o755, body: "id > /tmp/pwned"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:    "servers/a/in.zip",
				Destination:    "servers/a",
				ConflictPolicy: pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
			},
			wantError: "path escapes from parent",
		},
		{
			name:    "link_entry_pointing_at_the_neighbour",
			entries: []zipEntry{{name: "escape", mode: os.ModeSymlink | 0o777, body: "../b"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath: "servers/a/in.zip",
				Destination: "servers/a",
			},
			wantError: "symlink target escapes the destination",
		},
		{
			name:    "archive_read_through_a_link",
			entries: []zipEntry{{name: "x.txt", mode: 0o644, body: "x"}},
			params: &pb.ExtractArchiveParams{
				ArchivePath:       "servers/a/servers-link/b/victim.txt",
				Destination:       "servers/a/unpacked",
				Format:            pb.ArchiveFormat_ARCHIVE_FORMAT_GZ,
				CreateDestination: true,
			},
			wantError: "path escapes from parent",
		},
	}

	for modeName, opts := range confinementModes() {
		for _, tt := range tests {
			t.Run(modeName+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				workDir := tenantWorkDir(t)
				buildZipModes(t, filepath.Join(workDir, "servers", "a", "in.zip"), tt.entries)

				neighbour := readTree(t, filepath.Join(workDir, "servers", "b"))
				node := readTree(t, filepath.Join(workDir, "steamcmd"))

				res, err := Extract(context.Background(), workDir, tt.params, nil, opts...)

				assert.Equal(t, neighbour, readTree(t, filepath.Join(workDir, "servers", "b")))
				assert.Equal(t, node, readTree(t, filepath.Join(workDir, "steamcmd")))

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Nil(t, res)

					_, statErr := os.Lstat(filepath.Join(workDir, "servers", "a", "escape"))
					assert.ErrorIs(t, statErr, os.ErrNotExist, "an escaping link is not left on disk")

					return
				}

				require.NoError(t, err)

				for name, want := range tt.wantFiles {
					content, readErr := os.ReadFile(filepath.Join(workDir, "servers", "a", filepath.FromSlash(name)))
					require.NoError(t, readErr, name)
					assert.Equal(t, want, string(content), name)
				}
			})
		}
	}
}

func TestArchive_PathOutsideTheRequestRoot(t *testing.T) {
	t.Parallel()

	workDir := tenantWorkDir(t)
	buildZip(t, filepath.Join(workDir, "servers", "a", "in.zip"), map[string]string{"x.txt": "x"})

	const wantError = "path is outside its confinement root"

	_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "servers/a/leak.zip",
		BasePath:    "servers/b",
		Sources:     []string{"."},
	}, nil, fsutil.WithRoot("servers/a"))
	require.Error(t, err)
	assert.Equal(t, wantError, err.Error())

	_, err = Create(context.Background(), workDir, &pb.CreateArchiveParams{
		ArchivePath: "servers/b/planted.zip",
		BasePath:    "servers/a",
		Sources:     []string{"."},
	}, nil, fsutil.WithRoot("servers/a"))
	require.Error(t, err)
	assert.Equal(t, wantError, err.Error())

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath: "servers/a/in.zip",
		Destination: "servers/b",
	}, nil, fsutil.WithRoot("servers/a"))
	require.Error(t, err)
	assert.Equal(t, wantError, err.Error())

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath: "steamcmd/steamcmd.sh",
		Destination: "servers/a",
	}, nil, fsutil.WithRoot("servers/a"))
	require.Error(t, err)
	assert.Equal(t, wantError, err.Error())

	assert.Equal(t, map[string]string{
		"victim.txt":         "victim",
		"cstrike/server.cfg": "rcon_password secret",
	}, readTree(t, filepath.Join(workDir, "servers", "b")))

	_, statErr := os.Lstat(filepath.Join(workDir, "servers", "a", "leak.zip"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

// A caller acting for the node names no root: the archive and the tree may
// then live in different game server directories, each confined on its own.
func TestArchive_NodeLevelAcrossServerDirs(t *testing.T) {
	t.Parallel()

	opts := []fsutil.ConfineOption{fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})}

	t.Run("pack_a_server_into_a_node_directory", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "backups/a.zip",
			BasePath:    "servers/a",
			Sources:     []string{"."},
		}, nil, opts...)

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{
			"own.txt", "cstrike/", "cstrike/server.cfg", "servers-link", "node-link", "file-link", "in-link",
		}, zipEntryNames(t, filepath.Join(workDir, "backups", "a.zip")))
	})

	t.Run("packing_a_server_does_not_follow_its_links_out", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath:    "backups/a.zip",
			BasePath:       "servers/a",
			Sources:        []string{"."},
			FollowSymlinks: true,
		}, nil, opts...)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")

		_, statErr := os.Lstat(filepath.Join(workDir, "backups", "a.zip"))
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("unpack_a_node_archive_into_a_server", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)
		writeTree(t, workDir, map[string]string{"backups/.keep": ""})
		buildZipModes(t, filepath.Join(workDir, "backups", "in.zip"), []zipEntry{
			{name: "restored.cfg", mode: 0o644, body: "restored"},
		})

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath: "backups/in.zip",
			Destination: "servers/b",
		}, nil, opts...)

		require.NoError(t, err)

		content, err := os.ReadFile(filepath.Join(workDir, "servers", "b", "restored.cfg"))
		require.NoError(t, err)
		assert.Equal(t, "restored", string(content))
	})

	t.Run("unpacking_into_a_server_does_not_follow_its_links_out", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)
		writeTree(t, workDir, map[string]string{"backups/.keep": ""})
		buildZipModes(t, filepath.Join(workDir, "backups", "in.zip"), []zipEntry{
			{name: "node-link/steamcmd/steamcmd.sh", mode: 0o755, body: "id > /tmp/pwned"},
		})

		_, err := Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:    "backups/in.zip",
			Destination:    "servers/a",
			ConflictPolicy: pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
		}, nil, opts...)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")

		content, err := os.ReadFile(filepath.Join(workDir, "steamcmd", "steamcmd.sh"))
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh", string(content))
	})
}

// A caller acting for the node may start above the game server directories:
// pack one of them from the work directory, or all of them at once. The tree
// it reads still belongs to the tenants, so each game server directory it
// enters confines what is read below it, as if the request had started there.
func TestCreate_SourcesAboveTheServerDirs(t *testing.T) {
	t.Parallel()

	opts := []fsutil.ConfineOption{fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})}

	refused := []struct {
		name   string
		params *pb.CreateArchiveParams
	}{
		{
			name: "server_named_as_a_source",
			params: &pb.CreateArchiveParams{
				BasePath: ".", Sources: []string{"servers/a"}, FollowSymlinks: true,
			},
		},
		{
			name: "link_of_a_server_named_as_a_source",
			params: &pb.CreateArchiveParams{
				BasePath: ".", Sources: []string{"servers/a/file-link"}, FollowSymlinks: true,
			},
		},
		{
			name: "source_named_through_a_link_of_a_server",
			params: &pb.CreateArchiveParams{
				BasePath: ".", Sources: []string{"servers/a/servers-link/b/cstrike/server.cfg"},
			},
		},
		{
			name: "directory_holding_the_servers",
			params: &pb.CreateArchiveParams{
				BasePath: ".", Sources: []string{"servers"}, FollowSymlinks: true,
			},
		},
		{
			name: "whole_work_directory",
			params: &pb.CreateArchiveParams{
				BasePath: "", Sources: []string{"."}, FollowSymlinks: true,
			},
		},
		{
			name: "base_path_holding_the_servers",
			params: &pb.CreateArchiveParams{
				BasePath: "servers", Sources: []string{"a", "b"}, FollowSymlinks: true,
			},
		},
	}

	for _, tt := range refused {
		t.Run("refused/"+tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := tenantWorkDir(t)
			neighbour := readTree(t, filepath.Join(workDir, "servers", "b"))

			tt.params.ArchivePath = "backups/node.zip"

			res, err := Create(context.Background(), workDir, tt.params, nil, opts...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Nil(t, res)
			assert.Equal(t, neighbour, readTree(t, filepath.Join(workDir, "servers", "b")))

			_, statErr := os.Lstat(filepath.Join(workDir, "backups", "node.zip"))
			assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused operation leaves no archive behind")
		})
	}

	t.Run("links_are_packed_as_links", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "backups/node.zip",
			BasePath:    ".",
			Sources:     []string{"servers"},
		}, nil, opts...)

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{
			"servers/",
			"servers/a/", "servers/a/own.txt", "servers/a/cstrike/", "servers/a/cstrike/server.cfg",
			"servers/a/servers-link", "servers/a/node-link", "servers/a/file-link", "servers/a/in-link",
			"servers/b/", "servers/b/victim.txt", "servers/b/cstrike/", "servers/b/cstrike/server.cfg",
		}, zipEntryNames(t, filepath.Join(workDir, "backups", "node.zip")))
	})

	t.Run("links_staying_inside_their_server_are_followed", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)
		for _, link := range []string{"servers-link", "node-link", "file-link"} {
			require.NoError(t, os.Remove(filepath.Join(workDir, "servers", "a", link)))
		}

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath:    "backups/node.zip",
			BasePath:       ".",
			Sources:        []string{"servers", "steamcmd"},
			FollowSymlinks: true,
		}, nil, opts...)

		require.NoError(t, err)

		archive := filepath.Join(workDir, "backups", "node.zip")

		assert.ElementsMatch(t, []string{
			"servers/",
			"servers/a/", "servers/a/own.txt", "servers/a/cstrike/", "servers/a/cstrike/server.cfg",
			"servers/a/in-link/", "servers/a/in-link/server.cfg",
			"servers/b/", "servers/b/victim.txt", "servers/b/cstrike/", "servers/b/cstrike/server.cfg",
			"steamcmd/", "steamcmd/steamcmd.sh",
		}, zipEntryNames(t, archive))

		contents := zipContents(t, archive)
		assert.Equal(t, "hostname a", contents["servers/a/in-link/server.cfg"])
		assert.Equal(t, "rcon_password secret", contents["servers/b/cstrike/server.cfg"])
		assert.Equal(t, "#!/bin/sh", contents["steamcmd/steamcmd.sh"])
	})

	// A node-level link standing outside every game server directory leads
	// into one: what is read through it is read as that server's tree.
	t.Run("server_reached_through_a_node_link_is_confined", func(t *testing.T) {
		t.Parallel()

		workDir := tenantWorkDir(t)
		writeTree(t, workDir, map[string]string{"shortcuts/.keep": ""})
		require.NoError(t, os.Symlink("../servers/a", filepath.Join(workDir, "shortcuts", "current")))

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath:    "backups/node.zip",
			BasePath:       ".",
			Sources:        []string{"shortcuts"},
			FollowSymlinks: true,
		}, nil, opts...)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
	})
}

// A node-level link need not lead to a game server directory itself: it can end
// in the middle of one, at a directory that is no boundary. What is read
// through it still is that server's tree, so the links its tenant keeps there
// are followed inside the game server directory and nowhere else.
func TestCreate_NodeLinkIntoTheMiddleOfAServer(t *testing.T) {
	t.Parallel()

	opts := []fsutil.ConfineOption{fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"})}

	// newTree leaves server "a" with the links that stay inside it and adds
	// the node-level links into it.
	newTree := func(t *testing.T) string {
		t.Helper()

		workDir := tenantWorkDir(t)
		for _, link := range []string{"servers-link", "node-link", "file-link"} {
			require.NoError(t, os.Remove(filepath.Join(workDir, "servers", "a", link)))
		}

		writeTree(t, workDir, map[string]string{"shortcuts/.keep": ""})
		require.NoError(t, os.Symlink("../servers/a/cstrike", filepath.Join(workDir, "shortcuts", "current")))
		require.NoError(t, os.Symlink("../own.txt", filepath.Join(workDir, "servers", "a", "cstrike", "inside")))

		return workDir
	}

	requests := map[string]*pb.CreateArchiveParams{
		"directory_holding_the_link": {BasePath: ".", Sources: []string{"shortcuts"}},
		"link_named_as_a_source":     {BasePath: ".", Sources: []string{"shortcuts/current"}},
		"link_below_the_base_path":   {BasePath: "shortcuts", Sources: []string{"current"}},
		"link_named_as_the_base":     {BasePath: "shortcuts/current", Sources: []string{"."}},
		"whole_work_directory":       {BasePath: "", Sources: []string{"shortcuts", "steamcmd"}},
	}

	for name, params := range requests {
		t.Run("link_to_the_neighbour_is_refused/"+name, func(t *testing.T) {
			t.Parallel()

			workDir := newTree(t)
			require.NoError(t, os.Symlink(
				"../../b/cstrike/server.cfg", filepath.Join(workDir, "servers", "a", "cstrike", "leak"),
			))

			neighbour := readTree(t, filepath.Join(workDir, "servers", "b"))

			res, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath:    "backups/node.zip",
				BasePath:       params.GetBasePath(),
				Sources:        params.GetSources(),
				FollowSymlinks: true,
			}, nil, opts...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Nil(t, res)
			assert.Equal(t, neighbour, readTree(t, filepath.Join(workDir, "servers", "b")))

			_, statErr := os.Lstat(filepath.Join(workDir, "backups", "node.zip"))
			assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused operation leaves no archive behind")
		})

		t.Run("links_staying_inside_the_server_are_followed/"+name, func(t *testing.T) {
			t.Parallel()

			workDir := newTree(t)

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath:    "backups/node.zip",
				BasePath:       params.GetBasePath(),
				Sources:        params.GetSources(),
				FollowSymlinks: true,
			}, nil, opts...)
			require.NoError(t, err)

			bodies := slices.Collect(maps.Values(zipContents(t, filepath.Join(workDir, "backups", "node.zip"))))

			assert.Contains(t, bodies, "hostname a", "the server file behind the link")
			assert.Contains(t, bodies, "own", "the file a link inside the server leads to")
			assert.NotContains(t, bodies, "rcon_password secret")
		})
	}

	t.Run("file_link_led_on_by_the_tenant_is_refused", func(t *testing.T) {
		t.Parallel()

		workDir := newTree(t)
		require.NoError(t, os.Symlink("../servers/a/redirect.cfg", filepath.Join(workDir, "shortcuts", "config")))
		require.NoError(t, os.Symlink("../b/cstrike/server.cfg", filepath.Join(workDir, "servers", "a", "redirect.cfg")))

		res, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath:    "backups/node.zip",
			BasePath:       ".",
			Sources:        []string{"shortcuts/config"},
			FollowSymlinks: true,
		}, nil, opts...)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
		assert.Nil(t, res)
	})

	t.Run("link_is_packed_as_a_link_when_links_are_not_followed", func(t *testing.T) {
		t.Parallel()

		workDir := newTree(t)
		require.NoError(t, os.Symlink("a", filepath.Join(workDir, "servers", "alias")))

		_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
			ArchivePath: "backups/node.zip",
			BasePath:    ".",
			Sources:     []string{"shortcuts/current", "servers/alias"},
		}, nil, opts...)

		require.NoError(t, err)
		assert.ElementsMatch(
			t, []string{"shortcuts/current", "servers/alias"},
			zipEntryNames(t, filepath.Join(workDir, "backups", "node.zip")),
		)
	})
}

// A game server directory can hold another. Each is a boundary of its own for
// whatever is packed: a link the nested tenant keeps does not reach the files
// of the outer server, whichever directory the request started from.
func TestCreate_NestedServerDir(t *testing.T) {
	t.Parallel()

	opts := []fsutil.ConfineOption{
		fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/a/child", "servers/b"}),
	}

	// newTree leaves server "a" with the link that stays inside it and puts
	// server "child" into its directory.
	newTree := func(t *testing.T) string {
		t.Helper()

		workDir := tenantWorkDir(t)
		for _, link := range []string{"servers-link", "node-link", "file-link"} {
			require.NoError(t, os.Remove(filepath.Join(workDir, "servers", "a", link)))
		}

		writeTree(t, workDir, map[string]string{"servers/a/child/data.txt": "child"})
		require.NoError(t, os.Symlink("data.txt", filepath.Join(workDir, "servers", "a", "child", "inside")))
		require.NoError(t, os.Symlink("child", filepath.Join(workDir, "servers", "a", "jump")))

		return workDir
	}

	requests := map[string]*pb.CreateArchiveParams{
		"nested_dir_named_from_the_work_dir":  {BasePath: ".", Sources: []string{"servers/a/child"}},
		"nested_dir_named_from_the_outer_dir": {BasePath: "servers/a", Sources: []string{"child"}},
		"nested_dir_as_the_base":              {BasePath: "servers/a/child", Sources: []string{"."}},
		"link_of_the_outer_dir_to_the_nested": {BasePath: "servers/a", Sources: []string{"jump"}},
		"outer_dir":                           {BasePath: "servers/a", Sources: []string{"."}},
		"directory_holding_the_servers":       {BasePath: ".", Sources: []string{"servers"}},
	}

	for name, params := range requests {
		t.Run("link_into_the_outer_dir_is_refused/"+name, func(t *testing.T) {
			t.Parallel()

			workDir := newTree(t)
			require.NoError(t, os.Symlink("../own.txt", filepath.Join(workDir, "servers", "a", "child", "leak")))

			res, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath:    "backups/node.zip",
				BasePath:       params.GetBasePath(),
				Sources:        params.GetSources(),
				FollowSymlinks: true,
			}, nil, opts...)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")
			assert.Nil(t, res)

			_, statErr := os.Lstat(filepath.Join(workDir, "backups", "node.zip"))
			assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused operation leaves no archive behind")
		})

		t.Run("links_staying_inside_the_nested_dir_are_followed/"+name, func(t *testing.T) {
			t.Parallel()

			workDir := newTree(t)

			_, err := Create(context.Background(), workDir, &pb.CreateArchiveParams{
				ArchivePath:    "backups/node.zip",
				BasePath:       params.GetBasePath(),
				Sources:        params.GetSources(),
				FollowSymlinks: true,
			}, nil, opts...)
			require.NoError(t, err)

			contents := zipContents(t, filepath.Join(workDir, "backups", "node.zip"))

			followed := 0

			for _, body := range contents {
				if body == "child" {
					followed++
				}
			}

			assert.GreaterOrEqual(t, followed, 2, "data.txt and the link to it: %v", contents)
		})
	}
}

// A walk lasts, and a game server directory can be removed and created again
// while it does. The directory the walk then enters under that name confines
// what is read below it, however long ago the walk first looked.
func TestSourceWalker_ServerDirReplacedDuringTheWalk(t *testing.T) {
	t.Parallel()

	workDir := tenantWorkDir(t)
	for _, link := range []string{"servers-link", "node-link", "file-link"} {
		require.NoError(t, os.Remove(filepath.Join(workDir, "servers", "a", link)))
	}

	confiner := fsutil.NewConfiner(workDir, fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"}))

	base, err := confiner.Resolve(".", fsutil.TargetTree)
	require.NoError(t, err)
	defer base.Close()

	w := &sourceWalker{
		ctx:      context.Background(),
		confiner: confiner,
		limits:   &walkLimits{follow: true, maxEntries: defaultMaxFilesLimit()},
	}
	defer w.close()

	_, err = w.collect(base, ".", []string{"steamcmd"})
	require.NoError(t, err)

	former, err := os.Stat(filepath.Join(workDir, "servers", "b"))
	require.NoError(t, err)
	require.True(t, confiner.IsServerDir(former))

	require.NoError(t, os.Rename(filepath.Join(workDir, "servers", "b"), filepath.Join(workDir, "removed-b")))
	require.NoError(t, os.Mkdir(filepath.Join(workDir, "servers", "b"), 0o755))
	require.NoError(t, os.Symlink("../a/own.txt", filepath.Join(workDir, "servers", "b", "leak")))

	entries, err := w.collect(base, ".", []string{"servers"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
	assert.Empty(t, entries)
}

// Many sources, and many links to one game server directory, are read through
// the roots already open: the walk must not hold a descriptor for each.
func TestSourceWalker_SharesRoots(t *testing.T) {
	t.Parallel()

	workDir := tenantWorkDir(t)
	for _, link := range []string{"servers-link", "node-link", "file-link"} {
		require.NoError(t, os.Remove(filepath.Join(workDir, "servers", "a", link)))
	}

	writeTree(t, workDir, map[string]string{"shortcuts/.keep": ""})
	for _, name := range []string{"one", "two", "three"} {
		require.NoError(t, os.Symlink("../servers/a", filepath.Join(workDir, "shortcuts", name)))
	}

	confiner := fsutil.NewConfiner(workDir, fsutil.WithServerDirs(knownServerDirs{"servers/a", "servers/b"}))

	base, err := confiner.Resolve(".", fsutil.TargetTree)
	require.NoError(t, err)
	defer base.Close()

	w := &sourceWalker{
		ctx:      context.Background(),
		confiner: confiner,
		limits:   &walkLimits{follow: true, maxEntries: defaultMaxFilesLimit()},
	}
	defer w.close()

	entries, err := w.collect(base, ".", []string{
		"servers/a/own.txt", "servers/a/cstrike", "servers/a", "shortcuts", "steamcmd", "servers/b/victim.txt",
	})
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	assert.Len(t, w.scopes, 3, "the work directory and the two game server directories")

	for _, e := range entries {
		content, readErr := e.root.ReadFile(e.rel)
		if e.info.Mode().IsRegular() {
			require.NoError(t, readErr, e.name)
			assert.NotNil(t, content)
		}
	}
}
