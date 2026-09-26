package gameservercommands_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/test/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lifecycleSpy records the lifecycle calls that reach the process manager. Any
// other call hits the nil embedded interface and fails the test loudly.
type lifecycleSpy struct {
	contracts.ProcessManager

	mu    sync.Mutex
	calls []string
}

func (pm *lifecycleSpy) record(call string) (domain.Result, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.calls = append(pm.calls, call)

	return domain.SuccessResult, nil
}

func (pm *lifecycleSpy) Start(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return pm.record("start")
}

func (pm *lifecycleSpy) Stop(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return pm.record("stop")
}

func (pm *lifecycleSpy) Restart(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return pm.record("restart")
}

func (pm *lifecycleSpy) Status(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	return pm.record("status")
}

func (pm *lifecycleSpy) Calls() []string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	return append([]string(nil), pm.calls...)
}

func givenLifecycleCommand(
	t *testing.T, restartScript string, command domain.ServerCommand, server *domain.Server,
) (contracts.GameServerCommand, *lifecycleSpy) {
	t.Helper()

	cfg := &config.Config{
		WorkPath: t.TempDir(),
		Scripts: config.Scripts{
			Start:   "{command}",
			Stop:    "{command}",
			Status:  "{command}",
			Restart: restartScript,
		},
	}
	pm := &lifecycleSpy{}
	factory := gameservercommands.NewFactory(cfg, mocks.NewServerRepository(), components.NewExecutor(), pm)

	return factory.LoadServerCommand(command, server), pm
}

// A server suspended in the panel must not come up through a command the panel
// sent before the suspension reached it, nor through a restart: the default
// restart script goes straight to the process manager, and the stop/start path
// must not stop the server first only to be refused on the start.
func TestSuspendedServer_StartAndRestartAreRefused(t *testing.T) {
	tests := []struct {
		name          string
		command       domain.ServerCommand
		restartScript string
		wantError     string
	}{
		{
			name:      "start",
			command:   domain.Start,
			wantError: "[game_server_commands.defaultStartServer] start refused: server is blocked",
		},
		{
			name:          "restart through the process manager",
			command:       domain.Restart,
			restartScript: "{command}",
			wantError:     "[game_server_commands.defaultRestartServer] restart refused: server is blocked",
		},
		{
			name:          "restart through stop and start",
			command:       domain.Restart,
			restartScript: "",
			wantError:     "[game_server_commands.defaultRestartServer] restart refused: server is blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := givenSuspendedServerWithStartCommand(t, "./run.sh")
			cmd, pm := givenLifecycleCommand(t, tt.restartScript, tt.command, server)

			err := cmd.Execute(context.Background(), server)

			require.ErrorIs(t, err, domain.ErrServerBlocked)
			assert.Equal(t, tt.wantError, err.Error())
			assert.Equal(t, gameservercommands.ErrorResult, cmd.Result())
			assert.True(t, cmd.IsComplete())
			assert.Equal(
				t,
				"The server is suspended in the panel and cannot be started until the suspension is lifted.\n",
				string(cmd.ReadOutput()),
			)
			assert.Empty(t, pm.Calls())
		})
	}
}

// The same commands reach the process manager once the server is not
// suspended, which is what keeps the refusal test above from passing vacuously.
func TestNotSuspendedServer_StartAndRestartReachProcessManager(t *testing.T) {
	tests := []struct {
		name          string
		command       domain.ServerCommand
		restartScript string
		wantCalls     []string
	}{
		{
			name:      "start",
			command:   domain.Start,
			wantCalls: []string{"start"},
		},
		{
			name:          "restart through the process manager",
			command:       domain.Restart,
			restartScript: "{command}",
			wantCalls:     []string{"restart"},
		},
		{
			name:          "restart through stop and start",
			command:       domain.Restart,
			restartScript: "",
			wantCalls:     []string{"status", "stop", "start"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := givenServerWithStartCommand(t, "./run.sh")
			cmd, pm := givenLifecycleCommand(t, tt.restartScript, tt.command, server)

			err := cmd.Execute(context.Background(), server)

			require.NoError(t, err)
			assert.Equal(t, gameservercommands.SuccessResult, cmd.Result())
			assert.Equal(t, tt.wantCalls, pm.Calls())
		})
	}
}
