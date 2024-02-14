package gameservercommands

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type defaultStartServer struct {
	baseCommand

	startOutput io.ReadWriter

	loadServerCommand LoadServerCommandFunc

	updateCommand        contracts.GameServerCommand
	enableUpdatingBefore bool
}

func newDefaultStartServer(
	cfg *config.Config,
	executor contracts.Executor,
	processManager contracts.ProcessManager,
	loadServerCommand LoadServerCommandFunc,
) *defaultStartServer {
	return &defaultStartServer{
		baseCommand:       newBaseCommand(cfg, executor, processManager),
		startOutput:       components.NewSafeBuffer(),
		loadServerCommand: loadServerCommand,
	}
}

func (cmd *defaultStartServer) Execute(ctx context.Context, server *domain.Server) error {
	if cmd.enableUpdatingBefore && server.UpdateBeforeStart() {
		updateCmd := cmd.loadServerCommand(domain.Update, server)

		if updateCmd != nil {
			cmd.updateCommand = updateCmd
			err := updateCmd.Execute(ctx, server)
			if err != nil {
				cmd.SetComplete()
				return errors.WithMessage(
					err,
					"[game_server_commands.defaultStartServer] failed to update server before start",
				)
			}
		}
	}

	result, err := cmd.processManager.Start(ctx, server, cmd.startOutput)
	cmd.SetResult(int(result))
	cmd.SetComplete()

	if err != nil {
		return errors.WithMessage(
			err,
			"[game_server_commands.defaultStartServer] failed to execute start command",
		)
	}

	server.AffectStart()

	return nil
}

func (cmd *defaultStartServer) ReadOutput() []byte {
	var out []byte

	if cmd.updateCommand != nil {
		out = cmd.updateCommand.ReadOutput()
	}

	startOut, err := io.ReadAll(cmd.startOutput)
	if err != nil {
		return nil
	}
	out = append(out, startOut...)

	return out
}
