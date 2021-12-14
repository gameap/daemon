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

type restartServer struct {
	baseCommand
	bufCommand

	statusServer *statusServer
	stopServer   *stopServer
	startServer  *startServer
}

func newRestartServer(
	cfg *config.Config,
	executor contracts.Executor,
	statusServer *statusServer,
	stopServer *stopServer,
	startServer *startServer,
) *restartServer {
	cmd := &restartServer{
		baseCommand:  newBaseCommand(cfg, executor),
		bufCommand:   bufCommand{output: components.NewSafeBuffer()},
		statusServer: statusServer,
		stopServer:   stopServer,
		startServer:  startServer,
	}

	return cmd
}

func (s *restartServer) Execute(ctx context.Context, server *domain.Server) error {
	s.output = components.NewSafeBuffer()

	if s.cfg.Scripts.Restart == "" {
		return s.restartViaStopStart(ctx, server)
	}

	command := makeFullCommand(s.cfg, server, s.cfg.Scripts.Restart, server.StartCommand())

	result, err := s.executor.ExecWithWriter(ctx, command, s.output, contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(s.cfg),
		FallbackWorkDir: s.cfg.WorkDir(),
	})
	s.SetResult(result)
	s.SetComplete()

	return err
}

func (s *restartServer) restartViaStopStart(ctx context.Context, server *domain.Server) error {
	defer s.SetComplete()

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
			s.SetResult(s.stopServer.Result())
			return nil
		}
	}

	err = s.startServer.Execute(ctx, server)
	if err != nil {
		return err
	}

	s.SetResult(s.startServer.Result())

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
