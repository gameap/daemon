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
	cfg              *config.Config
	executor         contracts.Executor
	detailedExecutor contracts.Executor
}

func NewSimple(cfg *config.Config, executor, detailedExecutor contracts.Executor) *Simple {
	return &Simple{
		cfg:              cfg,
		executor:         executor,
		detailedExecutor: detailedExecutor,
	}
}

func (pm *Simple) Install(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	// Nothing to do here
	return domain.SuccessResult, nil
}

func (pm *Simple) Uninstall(_ context.Context, _ *domain.Server, _ io.Writer) (domain.Result, error) {
	// Nothing to do here
	return domain.SuccessResult, nil
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
	result, err := pm.executor.ExecWithWriter(
		ctx,
		domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.GetConsole, ""),
		out,
		pm.executeOptions(server),
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
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
	result, err := pm.detailedExecutor.ExecWithWriter(
		ctx,
		command,
		out,
		pm.executeOptions(server),
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Simple) executeOptions(server *domain.Server) contracts.ExecutorOptions {
	return contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(pm.cfg),
		FallbackWorkDir: pm.cfg.WorkDir(),
	}
}
