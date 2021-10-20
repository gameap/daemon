package game_server_commands

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
)

type startServer struct {
	baseCommand
	bufCommand
}

func newStartServer(cfg *config.Config) *startServer {
	return &startServer{
		baseCommand{
			cfg: cfg,
			complete: false,
			result: UnknownResult,
		},
		bufCommand{output: components.NewSafeBuffer()},
	}
}

func (s *startServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.ScriptStart, server.StartCommand())
	path := makeFullServerPath(s.cfg, server.Dir())

	var err error
	s.result, err = components.ExecWithWriter(ctx, command, path, s.output)
	s.complete = true
	if err != nil {
		return err
	}

	return nil
}
