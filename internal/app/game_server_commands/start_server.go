package gameservercommands

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type startServer struct {
	baseCommand

	startOutput io.ReadWriter

	loadServerCommand LoadServerCommandFunc

	updateCommand        contracts.GameServerCommand
	enableUpdatingBefore bool
}

func newStartServer(
	cfg *config.Config,
	executor contracts.Executor,
	loadServerCommand LoadServerCommandFunc,
) *startServer {
	return &startServer{
		baseCommand:       newBaseCommand(cfg, executor),
		startOutput:       components.NewSafeBuffer(),
		loadServerCommand: loadServerCommand,
	}
}

func (s *startServer) Execute(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Start, server.StartCommand())

	if s.enableUpdatingBefore && server.UpdateBeforeStart() {
		updateCmd := s.loadServerCommand(domain.Update)

		if updateCmd != nil {
			s.updateCommand = updateCmd
			err := updateCmd.Execute(ctx, server)
			if err != nil {
				s.SetComplete()
				return errors.WithMessage(err, "[game_server_commands.startServer] failed to update server before start")
			}
		}
	}

	result, err := s.executor.ExecWithWriter(ctx, command, s.startOutput, contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(s.cfg),
		FallbackWorkDir: s.cfg.WorkPath,
		Username:        server.User(),
	})

	s.SetResult(result)
	s.SetComplete()

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
