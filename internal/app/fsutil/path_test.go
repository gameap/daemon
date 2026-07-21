package fsutil

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootRel(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "relative_path", in: "servers/x", want: "servers/x"},
		{name: "empty_is_root", in: "", want: "."},
		{name: "dot_is_root", in: ".", want: "."},
		{name: "leading_slash_stripped", in: "/servers/x", want: "servers/x"},
		{name: "trailing_slash_cleaned", in: "servers/x/", want: "servers/x"},
		{name: "interior_dotdot_kept_when_local", in: "a/../b", want: "b"},
		{name: "escape_via_dotdot_rejected", in: "../etc/passwd", wantErr: true},
		{name: "escape_after_collapse_rejected", in: "a/../../b", wantErr: true},
		{name: "lone_dotdot_rejected", in: "..", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RootRel(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrPathOutsideRoot)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRootRel_StripsLeadingBackslash(t *testing.T) {
	got, err := RootRel(`\servers\x`)
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		assert.Equal(t, "servers/x", got)
	} else {
		// On unix a backslash is an ordinary filename character; only the
		// leading separator is trimmed.
		assert.Equal(t, `servers\x`, got)
	}
}

func TestRootRel_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only volume handling")
	}

	t.Run("drive_letter_stripped_and_treated_as_relative", func(t *testing.T) {
		got, err := RootRel(`C:\gameap\servers\x`)
		require.NoError(t, err)
		assert.Equal(t, "gameap/servers/x", got)
	})

	t.Run("dotdot_escape_still_rejected", func(t *testing.T) {
		_, err := RootRel(`..\Windows\System32`)
		require.ErrorIs(t, err, ErrPathOutsideRoot)
	})
}
