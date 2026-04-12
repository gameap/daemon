package processmanager

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
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
		Env:             server.EnvironmentVars(),
	}
}

func (pm *Simple) Attach(
	ctx context.Context, server *domain.Server, in io.Reader, out io.Writer,
) error {
	status, err := pm.Status(ctx, server, io.Discard)
	if err != nil {
		return errors.WithMessage(err, "failed to check server status")
	}
	if status != domain.SuccessResult {
		return ErrServiceNotRunning
	}

	lines := make(chan string, 1)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		for {
			select {
			case <-gctx.Done():
				return nil
			case line, ok := <-lines:
				if !ok {
					return nil
				}
				_, sendErr := pm.SendInput(gctx, line, server, io.Discard)
				if sendErr != nil {
					if errors.Is(sendErr, context.Canceled) {
						return nil
					}
					return errors.WithMessage(sendErr, "failed to send input")
				}
			}
		}
	})

	g.Go(func() error {
		var prevLen int
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-gctx.Done():
				return nil
			case <-ticker.C:
				var buf bytes.Buffer
				_, getErr := pm.GetOutput(gctx, server, &buf)
				if getErr != nil {
					if errors.Is(getErr, context.Canceled) {
						return nil
					}
					return errors.WithMessage(getErr, "failed to get output")
				}
				if buf.Len() > prevLen {
					if _, writeErr := out.Write(buf.Bytes()[prevLen:]); writeErr != nil {
						return errors.WithMessage(writeErr, "failed to write output")
					}
					prevLen = buf.Len()
				} else if buf.Len() < prevLen {
					prevLen = buf.Len()
				}
			}
		}
	})

	g.Go(func() error {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-gctx.Done():
				return nil
			case <-ticker.C:
				s, _ := pm.Status(gctx, server, io.Discard)
				if s != domain.SuccessResult {
					return nil
				}
			}
		}
	})

	err = g.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}

func (pm *Simple) HasOwnInstallation(_ *domain.Server) bool {
	return false
}
