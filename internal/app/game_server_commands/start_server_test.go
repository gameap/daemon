package gameservercommands_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/processmanager"
	"github.com/gameap/daemon/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartServer(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "../../../test/servers",
		Scripts: config.Scripts{
			Start: "{command}",
		},
	}
	var startCommand string
	if runtime.GOOS == "windows" {
		startCommand = "cmd /c run.bat"
	} else {
		startCommand = "./run.sh"
	}
	server := givenServerWithStartCommand(t, startCommand)
	startServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Start, server)

	err := startServerCommand.Execute(context.Background(), server)

	require.Nil(t, err)
	assert.Equal(t, gameservercommands.SuccessResult, startServerCommand.Result())
	assert.True(t, startServerCommand.IsComplete())
	assert.Contains(t, string(startServerCommand.ReadOutput()), "Server started")
}

func TestStartServer_ReadOutput(t *testing.T) {
	// Arrange
	cfg := &config.Config{
		WorkPath: "../../../test/servers",
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}
	var startCommand string
	if runtime.GOOS == "windows" {
		startCommand = "powershell ./run2.ps1"
	} else {
		startCommand = "./run2.sh"
	}
	server := givenServerWithStartCommand(t, startCommand)
	ctx, cancel := context.WithCancel(context.Background())
	startServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Start, server)
	go func() {
		err := startServerCommand.Execute(ctx, server)
		if err != nil {
			t.Error(err)
			return
		}
	}()
	time.Sleep(1 * time.Second)

	// Act #1
	out := startServerCommand.ReadOutput()

	// Assert #1
	assert.Contains(t, string(out), "Server starting...")
	assert.Contains(t, string(out), "Loading configuration...")
	assert.NotContains(t, string(out), "Server started")

	// Act #2
	time.Sleep(2 * time.Second)
	out = startServerCommand.ReadOutput()

	// Assert #2
	assert.Contains(t, string(out), "Server started")
	assert.NotContains(t, string(out), "Server starting...")
	assert.NotContains(t, string(out), "Loading configuration...")

	cancel()
}

func givenCommandFactory(t *testing.T, cfg *config.Config) *gameservercommands.ServerCommandFactory {
	t.Helper()

	executor := components.NewExecutor()

	return gameservercommands.NewFactory(
		cfg,
		mocks.NewServerRepository(),
		executor,
		processmanager.NewSimple(cfg, executor, executor),
	)
}

func givenServerWithStartCommand(t *testing.T, startCommand string) *domain.Server {
	t.Helper()

	return givenServerWithStartCommandAndVars(t, startCommand, map[string]string{})
}

func givenServerWithStartCommandAndVars(
	t *testing.T, startCommand string, vars map[string]string,
) *domain.Server {
	t.Helper()

	return domain.NewServer(
		1337,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{
			StartCode: "cstrike",
		},
		domain.GameMod{
			Name: "public",
		},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		"simple",
		"gameap-user",
		startCommand,
		"",
		"",
		"",
		true,
		time.Now(),
		vars,
		map[string]string{},
		time.Now(),
		0, // cpuLimit
		0, // ramLimit
	)
}

func TestStartServer_MissingWorkDir(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "../../../test/servers",
		Scripts: config.Scripts{
			Start: "{command}",
		},
	}
	server := givenServerWithStartCommandAndVars(t, "./run.sh", map[string]string{"work_dir": "missing"})
	startServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Start, server)

	err := startServerCommand.Execute(context.Background(), server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server process work directory does not exist")
	assert.Contains(t, err.Error(), `(work_dir "missing")`)
	assert.Contains(t, err.Error(), filepath.Join("simple", "missing"))
	assert.Equal(t, gameservercommands.ErrorResult, startServerCommand.Result())
	assert.True(t, startServerCommand.IsComplete())
}

func TestStartServer_WorkDirEscapesServerDir(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "../../../test/servers",
		Scripts: config.Scripts{
			Start: "{command}",
		},
	}
	server := givenServerWithStartCommandAndVars(t, "./run.sh", map[string]string{"work_dir": "../scripts"})
	startServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Start, server)

	err := startServerCommand.Execute(context.Background(), server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid work_dir "../scripts" from server vars`)
	assert.Contains(t, err.Error(), "path is outside work directory")
	assert.Equal(t, gameservercommands.ErrorResult, startServerCommand.Result())
	assert.True(t, startServerCommand.IsComplete())
}

func TestStartServer_RunsInWorkDir(t *testing.T) {
	workPath := t.TempDir()
	subDir := filepath.Join(workPath, "simple", "sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	var startCommand string
	if runtime.GOOS == "windows" {
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "run.bat"), []byte("@echo %cd%\r\n"), 0o644))
		startCommand = "cmd /c run.bat"
	} else {
		require.NoError(t, os.WriteFile(filepath.Join(subDir, "run.sh"), []byte("#!/bin/sh\npwd\n"), 0o755))
		startCommand = "./run.sh"
	}

	cfg := &config.Config{
		WorkPath: workPath,
		Scripts: config.Scripts{
			Start: "{command}",
		},
	}
	server := givenServerWithStartCommandAndVars(t, startCommand, map[string]string{"work_dir": "sub"})
	startServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Start, server)

	err := startServerCommand.Execute(context.Background(), server)

	require.NoError(t, err)
	assert.Equal(t, gameservercommands.SuccessResult, startServerCommand.Result())
	assert.Contains(t, string(startServerCommand.ReadOutput()), filepath.Join("simple", "sub"))
}
