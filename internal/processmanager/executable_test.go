package processmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executableName is the file name a start command would name on the running OS, together with
// the token a games catalogue entry spells it with.
func executableName() (fileName, bareToken string) {
	if runtime.GOOS == "windows" {
		return "server.exe", "server.exe"
	}

	return "server", "server"
}

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))

	return path
}

// A command with a path separator is the case supervisors get wrong: they resolve it against
// their own working directory rather than the one the game server is given.
func TestResolveCommandExecutable_RelativePathWithSeparator(t *testing.T) {
	workDir := t.TempDir()
	subDir := filepath.Join(workDir, "bin")
	require.NoError(t, os.Mkdir(subDir, 0o755))

	fileName, _ := executableName()
	want := writeExecutable(t, subDir, fileName)

	// "." is spelled out rather than joined: filepath.Join cleans it away, and the leading
	// separator is the whole point of the case.
	tests := map[string]struct {
		cmd string
		dir string
	}{
		"dot_prefixed":  {cmd: "." + string(filepath.Separator) + fileName, dir: subDir},
		"subdirectory":  {cmd: filepath.Join("bin", fileName), dir: workDir},
		"parent_walked": {cmd: filepath.Join("..", "bin", fileName), dir: subDir},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := resolveCommandExecutable(tt.cmd, tt.dir)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestResolveCommandExecutable_BareNameInWorkDir(t *testing.T) {
	workDir := t.TempDir()
	fileName, bareToken := executableName()
	want := writeExecutable(t, workDir, fileName)

	got, err := resolveCommandExecutable(bareToken, workDir)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestResolveCommandExecutable_AbsolutePath(t *testing.T) {
	workDir := t.TempDir()
	fileName, _ := executableName()
	want := writeExecutable(t, workDir, fileName)

	got, err := resolveCommandExecutable(want, t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// An interpreter the server directory does not hold has to stay reachable, or a start command
// such as `powershell -File run.ps1` would stop working.
func TestResolveCommandExecutable_FallsBackToPath(t *testing.T) {
	binDir := t.TempDir()
	fileName, bareToken := executableName()
	want := writeExecutable(t, binDir, fileName)

	t.Setenv("PATH", binDir)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}

	got, err := resolveCommandExecutable(bareToken, t.TempDir())

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// The server directory wins over PATH, so a server never runs a same-named binary from
// somewhere else on the host.
func TestResolveCommandExecutable_WorkDirWinsOverPath(t *testing.T) {
	workDir := t.TempDir()
	binDir := t.TempDir()
	fileName, bareToken := executableName()

	want := writeExecutable(t, workDir, fileName)
	writeExecutable(t, binDir, fileName)

	t.Setenv("PATH", binDir)
	if runtime.GOOS == "windows" {
		t.Setenv("PATHEXT", ".EXE")
	}

	got, err := resolveCommandExecutable(bareToken, workDir)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestResolveCommandExecutable_NotFound(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	_, err := resolveCommandExecutable("definitely-not-here", workDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "definitely-not-here")
	assert.Contains(t, err.Error(), workDir)
}
