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
	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Status, "")

	result, err := cmd.executor.ExecWithWriter(ctx, command, cmd.output, contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(cmd.cfg),
		FallbackWorkDir: cmd.cfg.WorkPath,
	})

	cmd.SetResult(result)
	cmd.SetComplete()

	return err
}
