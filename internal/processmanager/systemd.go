//go:build linux
// +build linux

package processmanager

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
)

const (
	systemdFilesDir    = ".systemd-services"
	systemdServicesDir = "/etc/systemd/system"
	servicePrefix      = "gameap-server-"

	// https://www.freedesktop.org/software/systemd/man/latest/systemctl.html#Exit%20status
	statusIsDeadPidExists  = 1
	statusIsDeadLockExists = 2
	statusNotRunning       = 3
	statusServiceUnknown   = 4

	outputSizeLimit = 30000
)

type SystemD struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewSystemD(cfg *config.Config, executor contracts.Executor) *SystemD {
	return &SystemD{
		cfg:      cfg,
		executor: executor,
	}
}

func (pm *SystemD) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	f, err := os.Create(pm.logFile(server))
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to create file")
	}
	err = f.Close()
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to close file")
	}

	if _, err = os.Stat(pm.stdinFile(server)); err == nil {
		err = os.Remove(pm.stdinFile(server))
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to remove file")
		}
	}

	return pm.command(ctx, server, "start", out)
}

func (pm *SystemD) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl stop %s", pm.socketName(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl stop %s", pm.serviceName(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	err = os.Remove(pm.socketFile(server))
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to remove socket file")
	}

	err = os.Remove(pm.serviceFile(server))
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to remove service file")
	}

	err = pm.daemonReload(ctx)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warn("Failed to daemon-reload")
	}

	return domain.Result(result), nil
}

func (pm *SystemD) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	return pm.command(ctx, server, "restart", out)
}

func (pm *SystemD) command(
	ctx context.Context, server *domain.Server, command string, out io.Writer,
) (domain.Result, error) {
	err := pm.makeService(ctx, server)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	if _, err := os.Stat(pm.socketFile(server)); errors.Is(err, os.ErrNotExist) {
		err := pm.makeSocket(ctx, server)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to make socket")
		}
	}

	err = pm.daemonReload(ctx)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to daemon-reload")
	}

	_, err = pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl %s %s", command, pm.socketName(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl %s %s", command, pm.serviceName(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *SystemD) daemonReload(ctx context.Context) error {
	_, _, err := pm.executor.Exec(
		ctx,
		"systemctl daemon-reload",
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	return err
}

func (pm *SystemD) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl status %s", pm.serviceName(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	switch result {
	case statusIsDeadPidExists,
		statusIsDeadLockExists,
		statusNotRunning,
		statusServiceUnknown:
		return domain.ErrorResult, nil
	case 0:
		return domain.SuccessResult, nil
	}

	return domain.ErrorResult, errors.New("unknown exit code")
}

func (pm *SystemD) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	f, err := os.Open(pm.logFile(server))
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to open file")
	}

	defer func() {
		err := f.Close()
		if err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close file"))
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to get file stat")
	}

	if stat.Size() > outputSizeLimit {
		_, err = f.Seek(-outputSizeLimit, io.SeekEnd)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to seek file")
		}
	}

	_, err = io.Copy(out, f)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to copy file")
	}

	return domain.SuccessResult, nil
}

func (pm *SystemD) SendInput(
	ctx context.Context, input string, server *domain.Server, _ io.Writer,
) (domain.Result, error) {
	f, err := os.OpenFile(pm.stdinFile(server), os.O_WRONLY, 0)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to open file")
	}

	defer func() {
		err := f.Close()
		if err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close file"))
		}
	}()

	_, err = f.WriteString(input + "\n")
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to write to file")
	}

	return domain.SuccessResult, nil
}

func (pm *SystemD) makeService(ctx context.Context, server *domain.Server) error {
	f, err := os.OpenFile(pm.serviceFile(server), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.WithMessage(err, "failed to open file")
	}
	defer func() {
		err := f.Close()
		if err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close file"))
		}
	}()

	c, err := pm.buildServiceConfig(server)
	if err != nil {
		return errors.WithMessage(err, "failed to build service config")
	}

	_, err = f.WriteString(c)
	if err != nil {
		return errors.WithMessage(err, "failed to write to file")
	}

	return nil
}

//nolint:funlen
func (pm *SystemD) buildServiceConfig(server *domain.Server) (string, error) {
	builder := strings.Builder{}
	builder.Grow(1000)

	// [Unit]
	builder.WriteString("[Unit]\n")

	builder.WriteString("Description=GameAP Server service (UUID ")
	builder.WriteString(server.UUID())
	builder.WriteString(")\n")

	builder.WriteString("After=network.target\n")

	builder.WriteString("Wants=network-online.target systemd-networkd-wait-online.service\n\n")

	// [Service]
	builder.WriteString("[Service]\n")

	builder.WriteString("Type=simple\n")

	builder.WriteString("ExecStart=")
	builder.WriteString(
		filepath.Join(
			server.WorkDir(pm.cfg),
			domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand()),
		),
	)
	builder.WriteString("\n")

	builder.WriteString("Sockets=")
	builder.WriteString(server.UUID())
	builder.WriteString(".socket\n")

	builder.WriteString("StandardInput=socket\n")

	logFile := pm.logFile(server)

	builder.WriteString("StandardOutput=file:")
	builder.WriteString(logFile)
	builder.WriteString("\n")

	builder.WriteString("StandardError=file:")
	builder.WriteString(logFile)
	builder.WriteString("\n")

	builder.WriteString("WorkingDirectory=")
	builder.WriteString(server.WorkDir(pm.cfg))
	builder.WriteString("\n")

	builder.WriteString("Restart=always\n")

	runAsUser, group, err := pm.user(server)
	if err != nil {
		return "", errors.WithMessage(err, "failed to get user")
	}

	builder.WriteString("User=")
	builder.WriteString(runAsUser)
	builder.WriteString("\n")

	builder.WriteString("Group=")
	builder.WriteString(group)
	builder.WriteString("\n\n")

	// [Install]
	builder.WriteString("[Install]\n")

	builder.WriteString("WantedBy=multi-user.target\n")

	return builder.String(), nil
}

func (pm *SystemD) makeSocket(ctx context.Context, server *domain.Server) error {
	f, err := os.OpenFile(pm.socketFile(server), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.WithMessage(err, "failed to open file")
	}
	defer func() {
		err := f.Close()
		if err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close file"))
		}
	}()

	_, err = f.WriteString(pm.buildSocketConfig(server))
	if err != nil {
		return errors.WithMessage(err, "failed to write to file")
	}

	return nil
}

func (pm *SystemD) buildSocketConfig(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(1000)

	// [Unit]
	builder.WriteString("[Unit]\n")

	builder.WriteString("Description=GameAP Server socket (UUID ")
	builder.WriteString(server.UUID())
	builder.WriteString(")\n\n")

	// [Socket]
	builder.WriteString("[Socket]\n")

	builder.WriteString("ListenFIFO=")
	builder.WriteString(pm.stdinFile(server))
	builder.WriteString("\n")

	builder.WriteString("Service=")
	builder.WriteString(servicePrefix)
	builder.WriteString(server.UUID())
	builder.WriteString(".service\n")

	return builder.String()
}

func (pm *SystemD) logFile(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(100)

	builder.WriteString(pm.cfg.WorkDir())
	builder.WriteRune(filepath.Separator)
	builder.WriteString(systemdFilesDir)
	builder.WriteRune(filepath.Separator)
	builder.WriteString(server.UUID())
	builder.WriteString(".log")

	return builder.String()
}

func (pm *SystemD) stdinFile(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(100)

	builder.WriteString(pm.cfg.WorkDir())
	builder.WriteRune(filepath.Separator)
	builder.WriteString(systemdFilesDir)
	builder.WriteRune(filepath.Separator)
	builder.WriteString(server.UUID())
	builder.WriteString(".stdin")

	return builder.String()
}

func (pm *SystemD) serviceName(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(50)

	builder.WriteString(servicePrefix)
	builder.WriteString(server.UUID())
	builder.WriteString(".service")

	return builder.String()
}

func (pm *SystemD) serviceFile(server *domain.Server) string {
	return filepath.Join(systemdServicesDir, pm.serviceName(server))
}

func (pm *SystemD) socketName(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(50)

	builder.WriteString(servicePrefix)
	builder.WriteString(server.UUID())
	builder.WriteString(".socket")

	return builder.String()
}

func (pm *SystemD) socketFile(server *domain.Server) string {
	return filepath.Join(systemdServicesDir, pm.socketName(server))
}

func (pm *SystemD) user(server *domain.Server) (string, string, error) {
	var systemUser *user.User
	var err error

	if server.User() != "" {
		systemUser, err = user.Lookup(server.User())
		if err != nil {
			return "", "", errors.WithMessagef(err, "failed to lookup user %s", server.User())
		}
	} else {
		systemUser, err = user.Current()
		if err != nil {
			return "", "", errors.WithMessage(err, "failed to get current user")
		}
	}

	return systemUser.Username, systemUser.Gid, nil
}
