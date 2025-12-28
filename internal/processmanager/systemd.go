//go:build linux

package processmanager

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
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

	stopTickerInterval = 500 * time.Millisecond
	stopTimeout        = 1 * time.Minute
)

type SystemD struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewSystemD(cfg *config.Config, _, detailedExecutor contracts.Executor) *SystemD {
	return &SystemD{
		cfg:      cfg,
		executor: detailedExecutor,
	}
}

func (pm *SystemD) Install(
	_ context.Context, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	// Nothing to do here
	return domain.SuccessResult, nil
}

func (pm *SystemD) Uninstall(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	s, err := pm.status(ctx, pm.serviceName(server), out)
	if err != nil {
		_, _ = out.Write([]byte("Failed to get service status: " + err.Error() + "\n"))
		logger.WithError(ctx, err).Warn("failed to get service status")
	} else if s == domain.SuccessResult {
		_, _ = out.Write([]byte("Service " + pm.serviceName(server) + " is running, stopping it first\n"))

		result, err := pm.Stop(ctx, server, out)
		if err != nil {
			_, _ = out.Write([]byte("Failed to stop service: " + err.Error() + "\n"))
			logger.WithError(ctx, err).Warn("failed to stop service")
		}

		if result != domain.SuccessResult {
			_, _ = out.Write([]byte("Failed to stop service, exit code: " + fmt.Sprint(result) + "\n"))
			logger.Logger(ctx).Warn("failed to stop service, exit code: " + fmt.Sprint(result))
		}
	}

	_, _ = out.Write([]byte("Removing socket file at " + pm.socketFile(server) + "\n"))
	err = os.Remove(pm.socketFile(server))
	if err != nil {
		_, _ = out.Write([]byte("Failed to remove socket file: " + err.Error() + "\n"))
		logger.WithError(ctx, err).Warn("failed to remove socket file")
	}

	_, _ = out.Write([]byte("Removing service file at " + pm.serviceFile(server) + "\n"))
	err = os.Remove(pm.serviceFile(server))
	if err != nil {
		_, _ = out.Write([]byte("Failed to remove service file: " + err.Error() + "\n"))
		logger.WithError(ctx, err).Warn("failed to remove service file")
	}

	err = pm.daemonReload(ctx)
	if err != nil {
		_, _ = out.Write([]byte("Failed to daemon-reload: " + err.Error() + "\n"))
		logger.Logger(ctx).WithError(err).Warn("Failed to daemon-reload")
	}

	return domain.SuccessResult, nil
}

func (pm *SystemD) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	logFile := pm.logFile(server)
	_, err := os.Stat(logFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return domain.ErrorResult, errors.Wrap(err, "failed to stat log file")
	}
	if errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(filepath.Dir(logFile), 0755)
		if err != nil {
			return domain.ErrorResult, errors.Wrap(err, "failed to create directory")
		}
	}

	f, err := os.Create(logFile)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to create file")
	}
	err = f.Close()
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to close file")
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

	if result != 0 {
		return domain.Result(result), nil
	}

	// Wait for service to stop
	_, _ = out.Write([]byte("Waiting for service to stop...\n"))
	if err := pm.waitForServiceStopped(ctx, server, out); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to wait for service to stop")
	}

	_, _ = out.Write([]byte("Service stopped\n"))
	return domain.SuccessResult, nil
}

func (pm *SystemD) waitForServiceStopped(ctx context.Context, server *domain.Server, out io.Writer) error {
	ticker := time.NewTicker(stopTickerInterval)
	defer ticker.Stop()

	timeout := time.After(stopTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return errors.New("timeout waiting for service to stop")
		case <-ticker.C:
			status, _ := pm.status(ctx, pm.serviceName(server), out)
			if status == domain.ErrorResult {
				// Service is not running (stopped)
				return nil
			}
		}
	}
}

func (pm *SystemD) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	return pm.command(ctx, server, "restart", out)
}

func (pm *SystemD) command(
	ctx context.Context, server *domain.Server, command string, out io.Writer,
) (domain.Result, error) {
	err := pm.makeService(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessagef(err, "failed to make service for server %d", server.ID())
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

	serviceName := pm.serviceName(server)
	socketName := pm.socketName(server)

	s, err := pm.status(ctx, socketName, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to get status")
	}

	if s != domain.SuccessResult {
		_, err = pm.executor.ExecWithWriter(
			ctx,
			fmt.Sprintf("systemctl start %s", socketName),
			out,
			contracts.ExecutorOptions{
				WorkDir: pm.cfg.WorkDir(),
			},
		)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
		}
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl %s %s", command, serviceName),
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
	return pm.status(ctx, pm.serviceName(server), out)
}

func (pm *SystemD) status(ctx context.Context, name string, out io.Writer) (domain.Result, error) {
	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("systemctl status %s", name),
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

func (pm *SystemD) makeService(ctx context.Context, server *domain.Server, out io.Writer) error {
	f, err := os.OpenFile(pm.serviceFile(server), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

	_, _ = out.Write([]byte("Creating service file at " + pm.serviceFile(server) + "\n"))
	_, _ = out.Write([]byte("----- BEGIN SERVICE FILE -----\n"))
	_, _ = out.Write([]byte(c + "\n"))
	_, _ = out.Write([]byte("----- END SERVICE FILE -----\n\n\n"))

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

	cmd, err := pm.makeStartCommand(server)
	if err != nil {
		return "", errors.WithMessage(err, "failed to make command")
	}
	builder.WriteString("ExecStart=")
	builder.WriteString(cmd)
	builder.WriteString("\n")

	builder.WriteString("Sockets=")
	builder.WriteString(server.UUID())
	builder.WriteString(".socket\n")

	builder.WriteString("StandardInput=socket\n")

	logFile := pm.logFile(server)

	builder.WriteString("StandardOutput=append:")
	builder.WriteString(logFile)
	builder.WriteString("\n")

	builder.WriteString("StandardError=append:")
	builder.WriteString(logFile)
	builder.WriteString("\n")

	builder.WriteString("WorkingDirectory=")
	builder.WriteString(server.WorkDir(pm.cfg))
	builder.WriteString("\n")

	builder.WriteString("Restart=always\n")

	runAsUser, group, err := pm.userAndGroup(server)
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

func (pm *SystemD) makeStartCommand(server *domain.Server) (string, error) {
	startCMD := domain.ReplaceShortCodes(server.StartCommand(), pm.cfg, server)

	if startCMD == "" {
		return "", ErrEmptyCommand
	}

	parts, err := shellquote.Split(startCMD)
	if err != nil {
		return "", errors.WithMessage(err, "failed to split command")
	}

	cmd := parts[0]
	args := parts[1:]

	var foundPath string

	if !filepath.IsAbs(cmd) {
		foundPath, err = exec.LookPath(filepath.Join(server.WorkDir(pm.cfg), cmd))
		if err != nil {
			foundPath, err = exec.LookPath(cmd)
			if err != nil {
				return "", errors.WithMessagef(err, "failed to find command '%s'", cmd)
			}
		}
	}

	if filepath.IsAbs(cmd) {
		foundPath, err = exec.LookPath(cmd)
		if err != nil {
			return "", errors.WithMessagef(err, "failed to find command '%s'", cmd)
		}
	}

	startCommand := shellquote.Join(append([]string{foundPath}, args...)...)

	return domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Start, startCommand), nil
}

func (pm *SystemD) makeSocket(ctx context.Context, server *domain.Server) error {
	f, err := os.OpenFile(pm.socketFile(server), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
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

// Full path to log file.
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

// Full path to stdin file.
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

func (pm *SystemD) userAndGroup(server *domain.Server) (string, string, error) {
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
