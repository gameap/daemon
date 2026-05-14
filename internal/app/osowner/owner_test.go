package osowner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptions_IsZero(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want bool
	}{
		{name: "empty_struct_is_zero", opts: Options{}, want: true},
		{name: "user_set_is_not_zero", opts: Options{User: "gameap"}, want: false},
		{name: "uid_set_is_not_zero", opts: Options{UID: 1000}, want: false},
		{name: "gid_set_is_not_zero", opts: Options{GID: 1000}, want: false},
		{name: "all_zero_values_is_zero", opts: Options{User: "", UID: 0, GID: 0}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.opts.IsZero())
		})
	}
}

func TestResolve_EmptyOptionsReturnsNotOk(t *testing.T) {
	uid, gid, ok, err := Resolve(Options{})

	require.NoError(t, err)
	assert.False(t, ok, "empty options must short-circuit to not-applicable")
	assert.Equal(t, 0, uid)
	assert.Equal(t, 0, gid)
}

func TestApplyToPath_EmptyOptionsIsNoop(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "file.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

	err := ApplyToPath(target, Options{})

	require.NoError(t, err)
}

func TestApplyToPath_NonRootDaemonIsNoop(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "file.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

	err := ApplyToPath(target, Options{User: "nonexistentuser_4j2k9c"})

	require.NoError(t, err, "non-root daemon must not even attempt user.Lookup")
}

func TestApplyRecursive_EmptyOptionsIsNoop(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(tempDir, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "a", "f.txt"), []byte("y"), 0o644))

	err := ApplyRecursive(tempDir, Options{})

	require.NoError(t, err)
}

func TestMissingSegments_TargetAlreadyExistsReturnsEmpty(t *testing.T) {
	tempDir := t.TempDir()

	segments, err := MissingSegments(tempDir)

	require.NoError(t, err)
	assert.Empty(t, segments, "existing target must have no missing segments")
}

func TestMissingSegments_AllSegmentsMissingReturnsAllInOrder(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "a", "b", "c")

	segments, err := MissingSegments(target)

	require.NoError(t, err)
	require.Len(t, segments, 3)
	assert.Equal(t, filepath.Join(tempDir, "a"), segments[0],
		"first missing segment must be the shallowest")
	assert.Equal(t, filepath.Join(tempDir, "a", "b"), segments[1])
	assert.Equal(t, target, segments[2],
		"last missing segment must be the deepest (target itself)")
}

func TestMissingSegments_SomeSegmentsExistReturnsOnlyMissing(t *testing.T) {
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "a", "b"), 0o755))
	target := filepath.Join(tempDir, "a", "b", "c", "d")

	segments, err := MissingSegments(target)

	require.NoError(t, err)
	require.Len(t, segments, 2)
	assert.Equal(t, filepath.Join(tempDir, "a", "b", "c"), segments[0])
	assert.Equal(t, target, segments[1])
}

func TestMissingSegments_TargetIsExistingFileReturnsEmpty(t *testing.T) {
	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "existing.txt")
	require.NoError(t, os.WriteFile(target, []byte("z"), 0o644))

	segments, err := MissingSegments(target)

	require.NoError(t, err)
	assert.Empty(t, segments)
}
