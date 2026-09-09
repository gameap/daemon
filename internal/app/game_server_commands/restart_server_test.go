package gameservercommands_test

import (
	"context"
	"testing"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A restart that stops the server first and only then notices the work directory would leave a
// running server down because of a configuration mistake, so the check has to reject the restart
// before anything is stopped. The stop/start path is the one at risk: it is used whenever no
// restart script is configured.
func TestRestartServer_MissingWorkDirDoesNotStopServer(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "../../../test/servers",
		Scripts: config.Scripts{
			Start:  "{command}",
			Stop:   "{command}",
			Status: "{command}",
		},
	}
	server := givenServerWithStartCommandAndVars(t, "./run.sh", map[string]string{"work_dir": "missing"})
	restartServerCommand := givenCommandFactory(t, cfg).LoadServerCommand(domain.Restart, server)

	err := restartServerCommand.Execute(context.Background(), server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "defaultRestartServer")
	assert.Contains(t, err.Error(), "server process work directory does not exist")
	assert.Equal(t, gameservercommands.ErrorResult, restartServerCommand.Result())
	assert.True(t, restartServerCommand.IsComplete())
	assert.Empty(t, restartServerCommand.ReadOutput())
}
