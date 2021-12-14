package gameservercommands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

type stopServer struct {
	baseCommand
	bufCommand
}

func newStopServer(cfg *config.Config, executor contracts.Executor) *stopServer {
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

	server.AffectStop()

	var err error
	s.result, err = s.executor.ExecWithWriter(ctx, command, s.output, contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(s.cfg),
		FallbackWorkDir: s.cfg.WorkDir(),
	})
	s.complete = true
	if err != nil {
		return err
	}

	return nil
}
