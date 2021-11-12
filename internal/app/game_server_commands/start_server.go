package game_server_commands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/pkg/errors"
)

type startServer struct {
	baseCommand
	bufCommand
}

func newStartServer(cfg *config.Config, executor interfaces.Executor) *startServer {
	return &startServer{
		baseCommand{
			cfg: cfg,
			executor: executor,
			complete: false,
			result: UnknownResult,
		},
		bufCommand{output: components.NewSafeBuffer()},
	}
}

func (s *startServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Start, server.StartCommand())
	path := makeFullServerPath(s.cfg, server.Dir())

	var err error
	s.result, err = s.executor.ExecWithWriter(ctx, command, s.output, components.ExecutorOptions{
		WorkDir: path,
	})
	s.complete = true
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.startServer] failed to execute start command")
	}

	return nil
}
