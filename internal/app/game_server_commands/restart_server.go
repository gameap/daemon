package game_server_commands

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type restartServer struct {
	baseCommand

	statusServer *statusServer
	stopServer   *stopServer
	startServer  *startServer

	output io.ReadWriter
}

func newRestartServer(
	cfg *config.Config,
	statusServer *statusServer,
	stopServer *stopServer,
	startServer *startServer,
) *restartServer {
	cmd := &restartServer{
		baseCommand: baseCommand{
			cfg:      cfg,
			complete: false,
			result:   UnknownResult,
		},
		statusServer: statusServer,
		stopServer: stopServer,
		startServer: startServer,
	}

	return cmd
}

func (s *restartServer) Execute(ctx context.Context, server *domain.Server) error {
	s.output = components.NewSafeBuffer()

	if s.cfg.Scripts.Restart == "" {
		return s.restartViaStopStart(ctx, server)
	}

	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Restart, server.RestartCommand())
	path := makeFullServerPath(s.cfg, server.Dir())

	var err error
	s.result, err = components.ExecWithWriter(ctx, command, path, s.output)
	s.complete = true
	if err != nil {
		return err
	}

	return nil
}

func (s *restartServer) restartViaStopStart(ctx context.Context, server *domain.Server) error {
	err := s.statusServer.Execute(ctx, server)
	if err != nil {
		return errors.WithMessage(err, "failed to check server status")
	}
	active := s.statusServer.Result() == SuccessResult

	if active {
		err = s.stopServer.Execute(ctx, server)
		if err != nil {
			return errors.WithMessage(err, "failed to stop server")
		}
		if s.stopServer.Result() != SuccessResult {
			s.complete = true
			s.result = s.stopServer.Result()
			return nil
		}
	}

	err = s.startServer.Execute(ctx, server)
	if err != nil {
		return err
	}
	if s.startServer.Result() != SuccessResult {
		s.complete = true
		s.result = s.startServer.Result()
		return nil
	}

	s.complete = true
	s.result = SuccessResult

	return nil
}

func (s *restartServer) ReadOutput() []byte {
	var err error
	var out []byte

	if s.cfg.Scripts.Restart == "" {
		statusOut := s.statusServer.ReadOutput()
		stopOut := s.stopServer.ReadOutput()
		startOut := s.startServer.ReadOutput()
		out = append(out, statusOut...)
		out = append(out, stopOut...)
		out = append(out, startOut...)
	} else {
		out, err = io.ReadAll(s.output)
		if err != nil {
			return []byte(err.Error())
		}
	}

	return out
}
