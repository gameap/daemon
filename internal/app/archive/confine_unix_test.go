//go:build linux || darwin

package archive

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
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
