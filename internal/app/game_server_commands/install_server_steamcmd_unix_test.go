//go:build linux || darwin

package gameservercommands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Whether the steamcmd directory lies in the work directory decides how it is
// opened: through the work directory, where a symlink leading out is refused,
// or by its path. The two are configured as names, and a name need not be the
// only one a directory has.
func TestRelativeToWorkDir(t *testing.T) {
	t.Parallel()

	// The layout, with no link in the names of real:
	//
	//	real/gameap                    the work directory
	//	real/gameap/steamcmd           a directory in it
	//	real/gameap/replaced           a symlink leading out, in place of a directory
	//	real/gameap/linked             a symlink leading out, on the way to a directory
	//	real/gameap-old/steamcmd       a directory next to it with a name that starts the same
	//	real/elsewhere/steamcmd        a directory out of it
	//	alias                          a symlink to real
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	real := filepath.Join(base, "real")
	alias := filepath.Join(base, "alias")

	for _, dir := range []string{"gameap/steamcmd", "gameap-old/steamcmd", "elsewhere/steamcmd"} {
		require.NoError(t, os.MkdirAll(filepath.Join(real, filepath.FromSlash(dir)), 0o755))
	}

	require.NoError(t, os.Symlink("../elsewhere/steamcmd", filepath.Join(real, "gameap", "replaced")))
	require.NoError(t, os.Symlink("../elsewhere", filepath.Join(real, "gameap", "linked")))
	require.NoError(t, os.Symlink("real", alias))

	tests := []struct {
		name    string
		workDir string
		dir     string
		wantRel string
		wantOK  bool
	}{
		{
			name:    "directory_in_the_work_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap", "steamcmd"),
			wantRel: "steamcmd",
			wantOK:  true,
		},
		{
			name:    "work_directory_itself",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap"),
			wantRel: ".",
			wantOK:  true,
		},
		{
			name:    "directory_that_is_not_there_yet",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap", "tools", "steamcmd"),
			wantRel: filepath.Join("tools", "steamcmd"),
			wantOK:  true,
		},
		{
			name:    "directory_out_of_the_work_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "elsewhere", "steamcmd"),
		},
		{
			name:    "directory_next_to_the_work_directory_with_a_name_that_starts_the_same",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap-old", "steamcmd"),
		},
		{
			name:    "directory_named_through_the_parent_of_the_work_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap", "..", "elsewhere", "steamcmd"),
		},
		{
			name:    "work_directory_configured_through_a_symlink",
			workDir: filepath.Join(alias, "gameap"),
			dir:     filepath.Join(real, "gameap", "steamcmd"),
			wantRel: "steamcmd",
			wantOK:  true,
		},
		{
			name:    "directory_configured_through_a_symlink",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(alias, "gameap", "steamcmd"),
			wantRel: "steamcmd",
			wantOK:  true,
		},
		{
			name:    "both_configured_through_a_symlink",
			workDir: filepath.Join(alias, "gameap"),
			dir:     filepath.Join(alias, "gameap", "steamcmd"),
			wantRel: "steamcmd",
			wantOK:  true,
		},
		{
			name:    "symlink_leading_out_in_place_of_the_directory_is_still_in_the_work_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap", "replaced"),
			wantRel: "replaced",
			wantOK:  true,
		},
		{
			name:    "symlink_leading_out_on_the_way_is_still_in_the_work_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     filepath.Join(real, "gameap", "linked", "steamcmd"),
			wantRel: filepath.Join("linked", "steamcmd"),
			wantOK:  true,
		},
		{
			name:    "relative_directory",
			workDir: filepath.Join(real, "gameap"),
			dir:     "steamcmd",
		},
		{
			name:    "directory_is_not_configured",
			workDir: filepath.Join(real, "gameap"),
			dir:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rel, ok := relativeToWorkDir(tt.workDir, tt.dir)

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantRel, rel)
		})
	}
}

// A relative steamcmd_path names a directory from wherever the daemon runs,
// and the daemon may well run in the work directory. The directory is in the
// work directory then, whatever its name looks like.
//
// Not parallel: the test changes the directory of the process.
func TestRelativeToWorkDir_RelativeDirectoryOfADaemonRunningInTheWorkDir(t *testing.T) {
	workDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	t.Chdir(workDir)

	for _, dir := range []string{"steamcmd", "./steamcmd", filepath.Join("tools", "..", "steamcmd")} {
		rel, ok := relativeToWorkDir(workDir, dir)

		assert.True(t, ok, dir)
		assert.Equal(t, "steamcmd", rel, dir)
	}

	_, ok := relativeToWorkDir(workDir, filepath.Join("..", "steamcmd"))
	assert.False(t, ok, "a relative name that leaves the work directory is out of it")

	_, ok = relativeToWorkDir(workDir, "")
	assert.False(t, ok, "a directory that is not configured is not the one the daemon runs in")
}

// A symlink in place of a steamcmd directory that is configured by a relative
// name is refused like any other, instead of being followed by its path.
//
// Not parallel: the test changes the directory of the process.
func TestInstallator_RelativeSteamCMDDirReplacedByASymlink(t *testing.T) {
	stand := givenInstallStand(t)
	stand.cfg.SteamCMDPath = "steamcmd"

	require.NoError(t, os.Symlink(stand.outside, filepath.Join(stand.cfg.WorkPath, "steamcmd")))

	t.Chdir(stand.cfg.WorkPath)

	err := stand.install("installer", steamRule())

	stand.assertOutsideUntouched(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "symlinks that lead out of it are not followed")
	assert.Empty(t, stand.executor.command, "steamcmd is not started")
}

// Whether the steamcmd directory holds the work directory decides whether its
// tree is shared with the group of the game servers. The two are compared as
// directories: a name need not be the only one a directory has.
func TestHoldsDir(t *testing.T) {
	t.Parallel()

	// The layout:
	//
	//	real/node                  the directory the root is opened at
	//	real/node/gameap           a directory in it
	//	real/node-old/gameap       a directory next to it with a name that starts the same
	//	real/elsewhere             a directory out of it
	//	real/elsewhere/into        a symlink to real/node/gameap
	//	alias                      a symlink to real
	base := t.TempDir()
	real := filepath.Join(base, "real")
	alias := filepath.Join(base, "alias")

	for _, dir := range []string{"node/gameap", "node-old/gameap", "elsewhere"} {
		require.NoError(t, os.MkdirAll(filepath.Join(real, filepath.FromSlash(dir)), 0o755))
	}

	require.NoError(t, os.Symlink("../node/gameap", filepath.Join(real, "elsewhere", "into")))
	require.NoError(t, os.Symlink("real", alias))

	root, err := os.OpenRoot(filepath.Join(real, "node"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{name: "directory_itself", dir: filepath.Join(real, "node"), want: true},
		{name: "directory_in_it", dir: filepath.Join(real, "node", "gameap"), want: true},
		{name: "directory_itself_named_through_a_symlink", dir: filepath.Join(alias, "node"), want: true},
		{name: "directory_in_it_named_through_a_symlink", dir: filepath.Join(alias, "node", "gameap"), want: true},
		{name: "symlink_elsewhere_that_leads_into_it", dir: filepath.Join(real, "elsewhere", "into"), want: true},
		{name: "directory_in_it_that_is_not_there_yet", dir: filepath.Join(real, "node", "servers", "new"), want: true},
		{name: "directory_above_it", dir: real},
		{name: "directory_out_of_it", dir: filepath.Join(real, "elsewhere")},
		{name: "directory_next_to_it_with_a_name_that_starts_the_same", dir: filepath.Join(real, "node-old", "gameap")},
		{name: "directory_out_of_it_that_is_not_there_yet", dir: filepath.Join(real, "elsewhere", "new")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := holdsDir(root, tt.dir)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
