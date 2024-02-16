package components_test

import (
	"context"
	"os"
	"runtime"
	"syscall"
	"testing"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtendableExecutor_ExecGetTool_ExpectToolDownloaded(t *testing.T) {
	tmpDir := givenTmp(t)
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Log(err)
		}
	}(tmpDir)
	executor := components.NewDefaultExtendableExecutor(components.NewCleanExecutor())
	executor.RegisterHandler("get-tool", customhandlers.NewGetTool(&config.Config{
		ToolsPath: tmpDir,
	}).Handle)

	result, code, err := executor.Exec(
		context.Background(),
		"get-tool https://raw.githubusercontent.com/gameap/scripts/master/fastdl/fastdl.sh",
		contracts.ExecutorOptions{},
	)

	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NotEmpty(t, result)
	path := tmpDir + "/fastdl.sh"
	assert.FileExists(t, path)
	assertFileIsExecutableByOwner(t, path)
}

func TestExtendableExecutor_ExecEchoCommand_ExpectCommandExecuted(t *testing.T) {
	var command string
	if runtime.GOOS == "windows" {
		command = "cmd /c echo hello"
	} else {
		command = "echo hello"
	}
	tmpDir := givenTmp(t)
	defer func(path string) {
		err := syscall.Rmdir(path)
		if err != nil {
			t.Log(err)
		}
	}(tmpDir)
	executor := components.NewDefaultExtendableExecutor(components.NewCleanExecutor())

	result, code, err := executor.Exec(
		context.Background(),
		command,
		contracts.ExecutorOptions{
			WorkDir: tmpDir,
		},
	)

	require.NoError(t, err)
	require.Equal(t, 0, code)
	if runtime.GOOS == "windows" {
		require.Equal(t, "hello\r\n", string(result))
	} else {
		require.Equal(t, "hello\n", string(result))
	}
}

func assertFileIsExecutableByOwner(t *testing.T, filePath string) bool {
	t.Helper()

	finfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	return finfo.Mode()&0100 != 0
}

func givenTmp(t *testing.T) string {
	t.Helper()

	workPath, err := os.MkdirTemp("", "extendable-executor-test")
	if err != nil {
		t.Fatal(err)
	}

	return workPath
}
