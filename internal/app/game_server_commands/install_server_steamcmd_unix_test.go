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
