package processmanager

import (
	"context"
	"fmt"
	"io"
	"os/user"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
)

const (
	defaultWidth        = 200
	defaultHistoryLimit = 1000
)

type Tmux struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewTmux(cfg *config.Config, executor contracts.Executor) *Tmux {
	return &Tmux{
		cfg:      cfg,
		executor: executor,
	}
}

func (pm *Tmux) Start(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	startCmd := domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand())

	startCmd = strconv.Quote(strings.ReplaceAll(startCmd, `\"`, `"`))

	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	tmuxCmd := fmt.Sprintf(`tmux new-session -d -s %s -x %d %s`, server.UUID(), defaultWidth, startCmd)

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`sh -c %s`, strconv.Quote(tmuxCmd)),
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	// Ignore result because it is not important for us
	_, err = pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`tmux set-option -g history-limit %d`, defaultHistoryLimit),
		out,
		options,
	)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warn("Failed to set history limit")
	}

	return domain.Result(result), nil
}

func (pm *Tmux) Stop(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`tmux kill-session -t %s`, server.UUID()),
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Tmux) Restart(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	statusResult, err := pm.Status(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to get server status")
	}

	if statusResult == domain.SuccessResult {
		_, err = pm.Stop(ctx, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to stop server")
		}
	}

	return pm.Start(ctx, server, out)
}

func (pm *Tmux) Status(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`tmux has-session -t %s`, server.UUID()),
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Tmux) GetOutput(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`tmux capture-pane -t %s -p`, server.UUID()),
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Tmux) SendInput(
	ctx context.Context, input string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	options, err := pm.executeOptions(server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "invalid server configuration")
	}

	input = strconv.Quote(strings.ReplaceAll(input, `\"`, `"`))

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf(`tmux send-keys -t %s %s ENTER`, server.UUID(), input),
		out,
		options,
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *Tmux) executeOptions(server *domain.Server) (contracts.ExecutorOptions, error) {
	systemUser, err := user.Lookup(server.User())
	if err != nil {
		return contracts.ExecutorOptions{}, errors.WithMessagef(err, "failed to lookup user %s", server.User())
	}

	return contracts.ExecutorOptions{
		WorkDir:         server.WorkDir(pm.cfg),
		FallbackWorkDir: systemUser.HomeDir,
		UID:             systemUser.Uid,
		GID:             systemUser.Gid,
	}, nil
}
