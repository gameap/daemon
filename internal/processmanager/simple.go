package processmanager

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type Simple struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewSimple(cfg *config.Config, executor contracts.Executor) *Simple {
	return &Simple{
		cfg:      cfg,
		executor: executor,
	}
}

func (pm *Simple) Start(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand()),
		out,
	)
}

func (pm *Simple) Stop(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Stop, server.StopCommand()),
		out,
	)
}

func (pm *Simple) Restart(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Restart, server.RestartCommand()),
		out,
	)
}

func (pm *Simple) Status(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Status, ""),
		out,
	)
}

func (pm *Simple) GetOutput(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.GetConsole, ""),
		out,
	)
}

func (pm *Simple) SendInput(
	ctx context.Context, input string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(
		ctx,
		server,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.SendCommand, input),
		out,
	)
}

func (pm *Simple) execCommand(
	ctx context.Context, server *domain.Server, command string, out io.Writer,
) (domain.Result, error) {
	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		command,
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Simple) executeOptions(server *domain.Server) (contracts.ExecutorOptions, error) {
	return contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(pm.cfg),
		FallbackWorkDir: pm.cfg.WorkDir(),
	}, nil
}
