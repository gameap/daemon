package processmanager

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

type Simple struct {
	cfg              *config.Config
	executor         contracts.Executor
	detailedExecutor contracts.Executor
}

func NewSimple(cfg *config.Config, executor, detailedExecutor contracts.Executor) *Simple {
	pm := &Simple{
		cfg:              cfg,
		executor:         executor,
		detailedExecutor: detailedExecutor,
	}

	pm.warnAboutUnusableScripts()

	return pm
}

// warnAboutUnusableScripts reports the scripts this process manager cannot run
// with the current configuration.
//
// Unlike every other process manager, simple has no built-in idea of how to talk
// to a game server: it only runs the scripts from the config. Two of them are
// invoked with no server-side command at all, so the default template
// "{command}" reduces to an empty argument vector and the call fails on every
// attempt. A missing status script is the damaging one — the servers loop treats
// a status it cannot evaluate as "leave the server alone", so the server is
// never reported to the panel and is never restarted after a crash. Failing here
// would be worse than warning, since start and stop may well be configured, but
// the operator has to be told once at startup rather than through a line in the
// log every five seconds.
func (pm *Simple) warnAboutUnusableScripts() {
	if pm.cfg == nil {
		return
	}

	unusable := make([]string, 0, 2)

	for _, script := range []struct {
		configKey string
		template  string
	}{
		{"scripts.status", pm.cfg.Scripts.Status},
		{"scripts.get_console", pm.cfg.Scripts.GetConsole},
	} {
		if templateIsEmptyWithoutCommand(script.template) {
			unusable = append(unusable, script.configKey)
		}
	}

	if len(unusable) == 0 {
		return
	}

	logger.Logger(context.Background()).Warnf(
		"process manager \"simple\" has no usable command for %s: "+
			"these scripts are called without a server command, so the default \"{command}\" "+
			"template expands to nothing. Set them in the daemon configuration, "+
			"otherwise server status cannot be determined and crashed servers will not be restarted",
		strings.Join(unusable, ", "),
	)
}

// templateIsEmptyWithoutCommand reports whether a command template produces no
// arguments at all once the {command} placeholder is substituted with nothing.
func templateIsEmptyWithoutCommand(template string) bool {
	tokens, err := shellquote.Split(template)
	if err != nil {
		return true
	}

	for _, token := range tokens {
		if token != "{command}" {
			return false
		}
	}

	return true
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
	return pm.execCommand(ctx, server, pm.cfg.Scripts.Start, server.StartCommand(), out)
}

func (pm *Simple) Stop(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(ctx, server, pm.cfg.Scripts.Stop, server.StopCommand(), out)
}

func (pm *Simple) Restart(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(ctx, server, pm.cfg.Scripts.Restart, server.RestartCommand(), out)
}

func (pm *Simple) Status(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	result, err := pm.execCommand(ctx, server, pm.cfg.Scripts.Status, "", out)
	if err != nil {
		return domain.UnknownResult, err
	}

	return statusFromExitCode(int(result))
}

func (pm *Simple) GetOutput(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	// The console is read with the plain executor: the detailed one prefixes the
	// command line and appends the exit code, which would pollute the output.
	return pm.execCommandWith(ctx, pm.executor, server, pm.cfg.Scripts.GetConsole, "", out)
}

func (pm *Simple) SendInput(
	ctx context.Context, input string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return pm.execCommand(ctx, server, pm.cfg.Scripts.SendCommand, input, out)
}

func (pm *Simple) execCommand(
	ctx context.Context, server *domain.Server, wrapper, serverCommand string, out io.Writer,
) (domain.Result, error) {
	return pm.execCommandWith(ctx, pm.detailedExecutor, server, wrapper, serverCommand, out)
}

func (pm *Simple) execCommandWith(
	ctx context.Context,
	executor contracts.Executor,
	server *domain.Server,
	wrapper, serverCommand string,
	out io.Writer,
) (domain.Result, error) {
	args, err := domain.BuildCommandArgs(pm.cfg, server, wrapper, serverCommand)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to build command")
	}

	result, err := executor.ExecWithWriterArgs(
		ctx,
		args,
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

// Metrics returns only the cached process-active gauge for now. PID-based
// resource collection (cpu/memory/io via gopsutil/process) is tracked as a
// follow-up since the simple process manager does not persist PIDs.
func (pm *Simple) Metrics(_ context.Context, server *domain.Server) ([]domain.Metric, error) {
	return []domain.Metric{livenessMetric(server, time.Now())}, nil
}
