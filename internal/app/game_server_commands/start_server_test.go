package gameservercommands_test

import (
	"context"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
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
	server := givenServerWithStartCommand(t, "./run.sh")
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
	server := givenServerWithStartCommand(t, "./run2.sh")
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

	return gameservercommands.NewFactory(
		cfg,
		mocks.NewServerRepository(),
		components.NewExecutor(),
	)
}

func givenServerWithStartCommand(t *testing.T, startCommand string) *domain.Server {
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
		map[string]string{
			"default_map": "de_dust2",
			"tickrate":    "1000",
		},
		map[string]string{},
		time.Now(),
	)
}
