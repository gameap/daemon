package gameservercommands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

type defaultStopServer struct {
	bufCommand
	baseCommand
}

func newDefaultStopServer(
	cfg *config.Config, executor contracts.Executor, processManager contracts.ProcessManager,
) *defaultStopServer {
	return &defaultStopServer{
		baseCommand: newBaseCommand(cfg, executor, processManager),
		bufCommand:  bufCommand{output: components.NewSafeBuffer()},
	}
}

func (cmd *defaultStopServer) Execute(ctx context.Context, server *domain.Server) error {
	server.AffectStop()

	result, err := cmd.processManager.Stop(ctx, server, cmd.output)
	cmd.SetResult(int(result))
	cmd.SetComplete()

	return err
}
