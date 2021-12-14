package components_test

import (
	"context"
	"io/ioutil"
	"os"
	"syscall"
	"testing"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtendableExecutor_ExecGetTool_ExpectToolDownloaded(t *testing.T) {
	tmpDir := givenTmp(t)
	defer func(path string) {
		err := syscall.Rmdir(path)
		if err != nil {
			t.Log(err)
		}
	}(tmpDir)
	cfg := &config.Config{
		ToolsPath: tmpDir,
	}
	executor := components.NewCleanDefaultExtendableExecutor(cfg)

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
	tmpDir := givenTmp(t)
	defer func(path string) {
		err := syscall.Rmdir(path)
		if err != nil {
			t.Log(err)
		}
	}(tmpDir)
	executor := components.NewCleanDefaultExtendableExecutor(&config.Config{})

	result, code, err := executor.Exec(
		context.Background(),
		"echo hello",
		contracts.ExecutorOptions{
			WorkDir: tmpDir,
		},
	)

	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.Equal(t, "hello\n", string(result))
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

	workPath, err := ioutil.TempDir("/tmp", "extendable-executor-test")
	if err != nil {
		t.Fatal(err)
	}

	return workPath
}
