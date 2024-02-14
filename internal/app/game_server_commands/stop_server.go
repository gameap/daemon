package gameservercommands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

type defaultStopServer struct {
	baseCommand
	bufCommand
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
	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Stop, server.StopCommand())

	server.AffectStop()

	result, err := cmd.executor.ExecWithWriter(ctx, command, cmd.output, contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(cmd.cfg),
		FallbackWorkDir: cmd.cfg.WorkDir(),
	})

	cmd.SetResult(result)
	cmd.SetComplete()

	return err
}
