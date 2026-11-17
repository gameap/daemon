//go:build linux || darwin

package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRename_LinksOutOfTheServerDirAreRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      string
		destination string
		// untouched lists files that must keep their content.
		untouched map[string]string
		absent    []string
	}{
		{
			name:        "neighbour_file_pulled_through_a_link",
			source:      "servers/a/servers-link/b/secret.cfg",
			destination: "servers/a/stolen.cfg",
			untouched:   map[string]string{"servers/b/secret.cfg": "rcon_password secret"},
			absent:      []string{"servers/a/stolen.cfg"},
		},
		{
			name:        "own_file_pushed_into_the_neighbour_through_a_link",
			source:      "servers/a/own.txt",
			destination: "servers/a/servers-link/b/planted.txt",
			untouched:   map[string]string{"servers/a/own.txt": "own"},
			absent:      []string{"servers/b/planted.txt"},
		},
		{
			name:        "node_file_replaced_through_a_link",
			source:      "servers/a/own.txt",
			destination: "servers/a/node-link/steamcmd/steamcmd.sh",
			untouched: map[string]string{
				"servers/a/own.txt":    "own",
				"steamcmd/steamcmd.sh": "#!/bin/sh",
			},
		},
		{
			name:        "between_servers_with_a_link_in_the_destination",
			source:      "servers/b/secret.cfg",
			destination: "servers/a/node-link/steamcmd/steamcmd.sh",
			untouched: map[string]string{
				"servers/b/secret.cfg": "rcon_password secret",
				"steamcmd/steamcmd.sh": "#!/bin/sh",
			},
		},
		{
			name:        "between_servers_with_a_link_in_the_source",
			source:      "servers/a/node-link/steamcmd/steamcmd.sh",
			destination: "servers/b/taken.sh",
			untouched:   map[string]string{"steamcmd/steamcmd.sh": "#!/bin/sh"},
			absent:      []string{"servers/b/taken.sh"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := confineTree(t)
			plantLinks(t, workDir)

			confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

			err := Rename(
				resolveEntry(t, confiner, tt.source),
				resolveEntry(t, confiner, tt.destination),
			)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "path escapes from parent")

			for name, want := range tt.untouched {
				content, readErr := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(name)))
				require.NoError(t, readErr, name)
				assert.Equal(t, want, string(content), name)
			}

			for _, name := range tt.absent {
				_, statErr := os.Lstat(filepath.Join(workDir, filepath.FromSlash(name)))
				assert.ErrorIs(t, statErr, os.ErrNotExist, name)
			}
		})
	}
}

// A rename acts on the link itself, so a tenant can still move or rename the
// links standing in their own directory, whatever they point at.
func TestRename_MovesTheLinkItself(t *testing.T) {
	t.Parallel()

	workDir := confineTree(t)
	plantLinks(t, workDir)

	confiner := NewConfiner(workDir, WithServerDirs(serverDirs{"servers/a", "servers/b"}))

	err := Rename(
		resolveEntry(t, confiner, "servers/a/servers-link"),
		resolveEntry(t, confiner, "servers/b/moved-link"),
	)

	require.NoError(t, err)

	target, err := os.Readlink(filepath.Join(workDir, "servers", "b", "moved-link"))
	require.NoError(t, err)
	assert.Equal(t, "..", target)

	_, err = os.Lstat(filepath.Join(workDir, "servers", "a", "servers-link"))
	assert.ErrorIs(t, err, os.ErrNotExist)
}
