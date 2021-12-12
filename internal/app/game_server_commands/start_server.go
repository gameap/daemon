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

	loadServerCommand LoadServerCommandFunc

	updateCommand        interfaces.GameServerCommand
	enableUpdatingBefore bool
}

func newStartServer(
	cfg *config.Config,
	executor interfaces.Executor,
	loadServerCommand LoadServerCommandFunc,
	enableUpdatingBefore bool,
) *startServer {
	return &startServer{
		baseCommand: baseCommand{
			cfg:      cfg,
			executor: executor,
			complete: false,
			result:   UnknownResult,
		},
		startOutput:          components.NewSafeBuffer(),
		loadServerCommand:    loadServerCommand,
		enableUpdatingBefore: enableUpdatingBefore,
	}
}

func (s *startServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Start, server.StartCommand())

	var err error

	if s.enableUpdatingBefore && server.UpdateBeforeStart() {
		updateCmd := s.loadServerCommand(domain.Update)
		s.updateCommand = updateCmd
		err = updateCmd.Execute(ctx, server)
		if err != nil {
			s.complete = true
			return errors.WithMessage(err, "[game_server_commands.startServer] failed to update server before start")
		}
	}

	s.result, err = s.executor.ExecWithWriter(ctx, command, s.startOutput, components.ExecutorOptions{
		WorkDir:         server.WorkDir(s.cfg),
		FallbackWorkDir: s.cfg.WorkPath,
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

	if s.updateCommand != nil {
		out = s.updateCommand.ReadOutput()
	}

	startOut, err := io.ReadAll(s.startOutput)
	if err != nil {
		return nil
	}
	out = append(out, startOut...)

	return out
}
