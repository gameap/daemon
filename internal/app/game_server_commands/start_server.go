package gameservercommands

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/pkg/errors"
)

type startServer struct {
	baseCommand

	startOutput io.ReadWriter

	update *installServer
}

func newStartServer(cfg *config.Config, executor interfaces.Executor, update *installServer) *startServer {
	return &startServer{
		baseCommand: baseCommand{
			cfg:      cfg,
			executor: executor,
			complete: false,
			result:   UnknownResult,
		},
		startOutput: components.NewSafeBuffer(),
		update:      update,
	}
}

func (s *startServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Start, server.StartCommand())
	path := makeFullServerPath(s.cfg, server.Dir())

	var err error

	if server.UpdateBeforeStart() && s.update != nil {
		err = s.update.Execute(ctx, server)
		if err != nil {
			s.complete = true
			return errors.WithMessage(err, "[game_server_commands.startServer] failed to update server before start")
		}
	}

	s.result, err = s.executor.ExecWithWriter(ctx, command, s.startOutput, components.ExecutorOptions{
		WorkDir: path,
	})
	s.complete = true
	if err != nil {
		return errors.WithMessage(err, "[game_server_commands.startServer] failed to execute start command")
	}

	server.AffectStart()

	return nil
}

func (s *startServer) ReadOutput() []byte {
	var out []byte

	if s.update != nil {
		out = s.update.ReadOutput()
	}

	startOut, err := io.ReadAll(s.startOutput)
	if err != nil {
		return nil
	}
	out = append(out, startOut...)

	return out
}
