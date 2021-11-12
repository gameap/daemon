package game_server_commands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type statusServer struct {
	baseCommand
	bufCommand
}

func newStatusServer(cfg *config.Config, executor interfaces.Executor) *statusServer {
	return &statusServer{
		baseCommand{
			cfg: cfg,
			executor: executor,
			complete: false,
			result: UnknownResult,
		},
		bufCommand{output: components.NewSafeBuffer()},
	}
}

func (s *statusServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Status, "")
	path := makeFullServerPath(s.cfg, server.Dir())

	var err error
	s.result, err = s.executor.ExecWithWriter(ctx, command, s.output, components.ExecutorOptions{
		WorkDir: path,
	})
	s.complete = true
	if err != nil {
		return err
	}

	return nil
}
