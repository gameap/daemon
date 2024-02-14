package gameservercommands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

type statusDefaultServer struct {
	baseCommand
	bufCommand
}

func newDefaultStatusServer(
	cfg *config.Config, executor contracts.Executor, processManager contracts.ProcessManager,
) *statusDefaultServer {
	return &statusDefaultServer{
		baseCommand: newBaseCommand(cfg, executor, processManager),
		bufCommand:  bufCommand{output: components.NewSafeBuffer()},
	}
}

func (cmd *statusDefaultServer) Execute(ctx context.Context, server *domain.Server) error {
	result, err := cmd.processManager.Status(ctx, server, cmd.output)
	cmd.SetResult(int(result))
	cmd.SetComplete()

	return err
}
