package gameservercommands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type stopServer struct {
	baseCommand
	bufCommand
}

func newStopServer(cfg *config.Config, executor interfaces.Executor) *stopServer {
	return &stopServer{
		baseCommand{
			cfg:      cfg,
			executor: executor,
			complete: false,
			result:   UnknownResult,
		},
		bufCommand{output: components.NewSafeBuffer()},
	}
}

func (s *stopServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Stop, server.StopCommand())
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