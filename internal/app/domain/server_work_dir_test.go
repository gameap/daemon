package domain

import (
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServerForWorkDir(
	vars map[string]string,
	gameModMetadata map[string]any,
	gameMetadata map[string]any,
) *Server {
	return NewServer(
		1,
		true,
		ServerInstalled,
		false,
		"test",
		"test-uuid",
		"test",
		Game{StartCode: "game", Metadata: gameMetadata},
		GameMod{Metadata: gameModMetadata},
		"127.0.0.1",
		27015,
		27016,
		27017,
		"rconpass",
		"server-dir",
		"",
		"",
		"",
		"",
		"",
		false,
		time.Time{},
		vars,
		Settings{},
		time.Time{},
		0,
		0,
	)
}

func TestResolveProcessWorkDir_Precedence(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		vars     map[string]string
		modMeta  map[string]any
		gameMeta map[string]any
		expected string
	}{
		{
			name:     "nothing_configured",
			goos:     "linux",
			expected: ".",
		},
		{
			name:     "generic_key_in_server_vars",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "bin"},
			expected: "bin",
		},
		{
			name:     "generic_key_in_game_mod_metadata",
			goos:     "linux",
			modMeta:  map[string]any{"work_dir": "mod/bin"},
			expected: "mod/bin",
		},
		{
			name:     "generic_key_in_game_metadata",
			goos:     "linux",
			gameMeta: map[string]any{"work_dir": "game/bin"},
			expected: "game/bin",
		},
		{
			name:     "server_vars_win_over_game_mod_metadata",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "from-vars"},
			modMeta:  map[string]any{"work_dir": "from-mod"},
			gameMeta: map[string]any{"work_dir": "from-game"},
			expected: "from-vars",
		},
		{
			name:     "game_mod_metadata_wins_over_game_metadata",
			goos:     "linux",
			modMeta:  map[string]any{"work_dir": "from-mod"},
			gameMeta: map[string]any{"work_dir": "from-game"},
			expected: "from-mod",
		},
		{
			name: "linux_key_wins_over_generic_on_linux",
			goos: "linux",
			modMeta: map[string]any{
				"work_dir":         "generic",
				"work_dir_linux":   "lin",
				"work_dir_windows": "win",
				"work_dir_macos":   "mac",
			},
			expected: "lin",
		},
		{
			name: "windows_key_wins_over_generic_on_windows",
			goos: "windows",
			modMeta: map[string]any{
				"work_dir":         "generic",
				"work_dir_linux":   "lin",
				"work_dir_windows": "win",
				"work_dir_macos":   "mac",
			},
			expected: "win",
		},
		{
			name: "macos_key_wins_over_generic_on_darwin",
			goos: "darwin",
			modMeta: map[string]any{
				"work_dir":         "generic",
				"work_dir_linux":   "lin",
				"work_dir_windows": "win",
				"work_dir_macos":   "mac",
			},
			expected: "mac",
		},
		{
			name: "darwin_does_not_fall_back_to_linux_key",
			goos: "darwin",
			modMeta: map[string]any{
				"work_dir":       "generic",
				"work_dir_linux": "lin",
			},
			expected: "generic",
		},
		{
			name: "other_unix_uses_linux_key",
			goos: "freebsd",
			modMeta: map[string]any{
				"work_dir":       "generic",
				"work_dir_linux": "lin",
			},
			expected: "lin",
		},
		{
			name:     "windows_key_is_ignored_on_linux",
			goos:     "linux",
			modMeta:  map[string]any{"work_dir_windows": "win"},
			gameMeta: map[string]any{"work_dir": "game"},
			expected: "game",
		},
		{
			name:     "generic_key_on_higher_level_wins_over_os_key_on_lower_level",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "from-vars"},
			modMeta:  map[string]any{"work_dir_linux": "from-mod-linux"},
			expected: "from-vars",
		},
		{
			name:     "empty_value_falls_through",
			goos:     "linux",
			vars:     map[string]string{"work_dir": ""},
			modMeta:  map[string]any{"work_dir": "mod/bin"},
			expected: "mod/bin",
		},
		{
			name:     "blank_value_falls_through",
			goos:     "linux",
			vars:     map[string]string{"work_dir_linux": "   "},
			modMeta:  map[string]any{"work_dir": "mod/bin"},
			expected: "mod/bin",
		},
		{
			name:     "non_string_metadata_is_skipped",
			goos:     "linux",
			modMeta:  map[string]any{"work_dir": 42, "work_dir_linux": true},
			gameMeta: map[string]any{"work_dir": "game/bin"},
			expected: "game/bin",
		},
		{
			name:     "dot_means_server_dir",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "."},
			modMeta:  map[string]any{"work_dir": "mod/bin"},
			expected: ".",
		},
		{
			name:     "dot_slash_means_server_dir",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "./"},
			expected: ".",
		},
		{
			name:     "trailing_and_leading_dot_segments_are_cleaned",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "./sub/"},
			expected: "sub",
		},
		{
			name:     "double_separators_are_cleaned",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "a//b"},
			expected: "a/b",
		},
		{
			name:     "inner_parent_segment_is_resolved",
			goos:     "linux",
			vars:     map[string]string{"work_dir": "a/../b"},
			expected: "b",
		},
		{
			name:     "nested_path",
			goos:     "windows",
			modMeta:  map[string]any{"work_dir_windows": "GroundBranch/Binaries/Win64"},
			expected: "GroundBranch/Binaries/Win64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, err := resolveProcessWorkDir(tt.goos, tt.vars, tt.modMeta, tt.gameMeta)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, rel)
		})
	}
}

func TestResolveProcessWorkDir_WindowsSeparators(t *testing.T) {
	rel, err := resolveProcessWorkDir(
		"windows", nil, map[string]any{"work_dir_windows": `GroundBranch\Binaries\Win64`}, nil,
	)

	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		assert.Equal(t, "GroundBranch/Binaries/Win64", rel)
	} else {
		// On other systems a backslash is an ordinary character, so the value is one segment.
		assert.Equal(t, `GroundBranch\Binaries\Win64`, rel)
	}
}

func TestResolveProcessWorkDir_EscapeOutsideServerDir(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"parent_segment", "../x"},
		{"parent_only", ".."},
		{"nested_parent_escape", "a/../../b"},
		{"trailing_parent_escape", "a/../.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveProcessWorkDir(
				"linux", nil, map[string]any{"work_dir_linux": tt.value}, nil,
			)

			require.Error(t, err)
			assert.ErrorIs(t, err, fsutil.ErrPathOutsideRoot)
			assert.Contains(t, err.Error(), "invalid work_dir_linux "+strconv.Quote(tt.value)+" from game mod metadata")
			assert.Contains(t, err.Error(), "path is outside work directory")
		})
	}
}

func TestResolveProcessWorkDir_AnchoredPathsAreRejected(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"unix_absolute", "/srv/gameap/servers/x"},
		{"backslash_absolute", `\servers\x`},
		{"windows_drive_absolute", `C:\servers\x`},
		{"windows_drive_relative", "C:servers"},
		{"windows_drive_forward_slash", "D:/servers/x"},
		{"unc_path", `\\srv\share\x`},
		{"unc_forward_slash", "//srv/share/x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveProcessWorkDir("linux", map[string]string{"work_dir": tt.value}, nil, nil)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrProcessWorkDirNotRelative)
			assert.Contains(t, err.Error(), "invalid work_dir "+strconv.Quote(tt.value)+" from server vars")
		})
	}
}

func TestResolveProcessWorkDir_InvalidCharactersAreRejected(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"newline", "bin\nExecStart=/bin/true"},
		{"carriage_return", "bin\rx"},
		{"tab", "bin\tx"},
		{"nul", "bin\x00x"},
		{"percent_specifier", "bin/100%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveProcessWorkDir("linux", nil, map[string]any{"work_dir": tt.value}, nil)

			require.Error(t, err)
			assert.ErrorIs(t, err, ErrProcessWorkDirInvalidCharacters)
			assert.Contains(t, err.Error(), "from game mod metadata")
		})
	}
}

func TestServer_ProcessWorkDirRel_UsesRunningOSKey(t *testing.T) {
	var osKey string

	switch runtime.GOOS {
	case "windows":
		osKey = WorkDirWindowsKey
	case "darwin":
		osKey = WorkDirMacOSKey
	default:
		osKey = WorkDirLinuxKey
	}

	server := newTestServerForWorkDir(nil, map[string]any{osKey: "os/bin", WorkDirKey: "generic"}, nil)

	rel, err := server.ProcessWorkDirRel()

	require.NoError(t, err)
	assert.Equal(t, "os/bin", rel)
}

func TestServer_ProcessWorkDir(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}

	tests := []struct {
		name     string
		vars     map[string]string
		modMeta  map[string]any
		expected string
	}{
		{
			name:     "defaults_to_server_dir",
			expected: filepath.Join("/work-path", "server-dir"),
		},
		{
			name:     "joins_relative_value_from_server_vars",
			vars:     map[string]string{"work_dir": "bin/x"},
			expected: filepath.Join("/work-path", "server-dir", "bin", "x"),
		},
		{
			name:     "joins_relative_value_from_game_mod_metadata",
			modMeta:  map[string]any{"work_dir": "mod/bin"},
			expected: filepath.Join("/work-path", "server-dir", "mod", "bin"),
		},
		{
			name:     "dot_is_server_dir",
			vars:     map[string]string{"work_dir": "."},
			expected: filepath.Join("/work-path", "server-dir"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServerForWorkDir(tt.vars, tt.modMeta, nil)

			dir, err := server.ProcessWorkDir(cfg)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, dir)
			assert.Equal(t, filepath.Join("/work-path", "server-dir"), server.WorkDir(cfg))
		})
	}
}

func TestServer_ProcessWorkDir_InvalidValue(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForWorkDir(map[string]string{"work_dir": "../other"}, nil, nil)

	_, err := server.ProcessWorkDir(cfg)

	require.Error(t, err)
	assert.ErrorIs(t, err, fsutil.ErrPathOutsideRoot)
}

func TestServer_ProcessWorkDirRel_SettingsOverrideVars(t *testing.T) {
	server := NewServer(
		1, true, ServerInstalled, false, "test", "test-uuid", "test",
		Game{}, GameMod{Vars: []GameModVarTemplate{{Key: "work_dir", DefaultValue: "from-mod-var"}}},
		"127.0.0.1", 27015, 27016, 27017, "rconpass", "server-dir", "",
		"", "", "", "", false, time.Time{},
		map[string]string{"work_dir": "from-vars"},
		Settings{"work_dir": "from-settings"},
		time.Time{}, 0, 0,
	)

	rel, err := server.ProcessWorkDirRel()

	require.NoError(t, err)
	assert.Equal(t, "from-settings", rel)
}
