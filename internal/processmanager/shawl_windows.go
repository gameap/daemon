//go:build windows

package processmanager

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/gameap/gameapctl/pkg/oscore"
	"github.com/pkg/errors"
)

const (
	shawlServicesConfigPath = "C:\\gameap\\services"
	shawlServicePrefix      = "gameapServer"
	shawlOutputSizeLimit    = 30000
	shawlStopTimeout        = "10000"
	shawlLogRotate          = "daily"
	shawlLogRetain          = "7"

	stopTickerInterval = 500 * time.Millisecond
	stopTimeout        = 1 * time.Minute
)

type Shawl struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewShawl(cfg *config.Config, _, detailedExecutor contracts.Executor) *Shawl {
	return &Shawl{
		cfg:      cfg,
		executor: detailedExecutor,
	}
}

func (pm *Shawl) Install(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	created, err := pm.makeService(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	if created {
		_, _ = out.Write([]byte("Service created successfully\n"))
	} else {
		_, _ = out.Write([]byte("Service configuration updated\n"))
	}

	return domain.SuccessResult, nil
}

func (pm *Shawl) Uninstall(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	serviceName := pm.serviceName(server)

	// Stop service first (ignore errors if not running)
	_, _ = pm.Stop(ctx, server, out)

	// Delete the service
	_, _ = out.Write([]byte("Deleting service " + serviceName + "\n"))
	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc delete %s", serviceName),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to delete service")
	}

	if result != 0 {
		logger.Warn(ctx, "sc delete returned non-zero exit code")
	}

	// Remove config file
	configFile := pm.configFile(server)
	if err := os.Remove(configFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.WithError(ctx, err).Warn("failed to remove service config file")
	}

	return domain.SuccessResult, nil
}

func (pm *Shawl) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if !pm.cfg.UseNetworkServiceUser {
		err := checkUser(server.User())
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to check user")
		}
	}

	// Ensure service exists and config is up to date
	_, err := pm.makeService(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	serviceName := pm.serviceName(server)
	_, _ = out.Write([]byte("Starting service " + serviceName + "\n"))

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc start %s", serviceName),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to start service")
	}

	// sc start returns 0 on success
	if result != 0 {
		return domain.ErrorResult, errors.New("failed to start service")
	}

	return domain.SuccessResult, nil
}

func (pm *Shawl) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	serviceName := pm.serviceName(server)
	_, _ = out.Write([]byte("Stopping service " + serviceName + "\n"))

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc stop %s", serviceName),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to stop service")
	}

	// sc stop returns 0 on success (or if already stopped in some cases)
	if result != 0 {
		// Check if service is already stopped
		status, _ := pm.Status(ctx, server, io.Discard)
		if status == domain.ErrorResult {
			// Service is not running, that's fine
			return domain.SuccessResult, nil
		}
		return domain.ErrorResult, errors.New("failed to stop service")
	}

	// Wait for service to stop
	_, _ = out.Write([]byte("Waiting for service to stop...\n"))
	if err := pm.waitForServiceStopped(ctx, server); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to wait for service to stop")
	}

	_, _ = out.Write([]byte("Service stopped\n"))
	return domain.SuccessResult, nil
}

func (pm *Shawl) waitForServiceStopped(ctx context.Context, server *domain.Server) error {
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
			status, _ := pm.Status(ctx, server, io.Discard)
			if status == domain.ErrorResult {
				// Service is not running (stopped)
				return nil
			}
		}
	}
}

func (pm *Shawl) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.Stop(ctx, server, out)
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to stop service during restart")
	}

	return pm.Start(ctx, server, out)
}

func (pm *Shawl) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	serviceName := pm.serviceName(server)

	// Check if config file exists
	if _, err := os.Stat(pm.configFile(server)); err != nil {
		logger.Debug(ctx, "Service config file not found")
		return domain.ErrorResult, nil
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc query %s", serviceName),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to query service status")
	}

	// sc query returns 0 if service exists
	// We need to parse output to check if RUNNING
	if result != 0 {
		// Service doesn't exist
		return domain.ErrorResult, nil
	}

	// For a more accurate check, we'd need to parse the output
	// But since we're writing to out, we can use a buffer to check
	return pm.checkServiceRunning(ctx, server)
}

func (pm *Shawl) checkServiceRunning(ctx context.Context, server *domain.Server) (domain.Result, error) {
	serviceName := pm.serviceName(server)

	var output strings.Builder
	_, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc query %s", serviceName),
		&output,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return domain.ErrorResult, nil
	}

	// Check if output contains "RUNNING"
	if strings.Contains(output.String(), "RUNNING") {
		return domain.SuccessResult, nil
	}

	return domain.ErrorResult, nil
}

func (pm *Shawl) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	logFile := pm.logPath(server)

	f, err := os.Open(logFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = out.Write([]byte(fmt.Sprintf("Log file %s does not exist\n", logFile)))
			return domain.SuccessResult, nil
		}
		return domain.ErrorResult, errors.WithMessage(err, "failed to open log file")
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close log file"))
		}
	}()

	stat, err := f.Stat()
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to get file stat")
	}

	if stat.Size() > shawlOutputSizeLimit {
		_, err = f.Seek(-shawlOutputSizeLimit, io.SeekEnd)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to seek file")
		}
	}

	// Parse shawl log format and extract message content
	// Format: 2025-11-29 00:07:35 [DEBUG] stdout: "message"
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		msg := parseShawlLogLine(line)
		if msg != "" {
			_, _ = out.Write([]byte(msg + "\n"))
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to read log file")
	}

	return domain.SuccessResult, nil
}

// parseShawlLogLine extracts the message content from a shawl log line.
// Input format: 2025-11-29 00:07:35 [DEBUG] stdout: "message"
// Output: message
func parseShawlLogLine(line string) string {
	// Find the position after the log level bracket, e.g., after "[DEBUG] "
	bracketEnd := strings.Index(line, "] ")
	if bracketEnd == -1 {
		return line
	}

	rest := line[bracketEnd+2:]

	// Find the colon after stdout/stderr
	colonPos := strings.Index(rest, ": ")
	if colonPos == -1 {
		return rest
	}

	msg := rest[colonPos+2:]

	// Remove surrounding quotes if present
	if len(msg) >= 2 && msg[0] == '"' && msg[len(msg)-1] == '"' {
		msg = msg[1 : len(msg)-1]
	}

	return msg
}

func (pm *Shawl) SendInput(
	_ context.Context, _ string, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.ErrorResult, errors.New("input is not supported on Windows")
}

func (pm *Shawl) makeService(ctx context.Context, server *domain.Server, out io.Writer) (bool, error) {
	serviceName := pm.serviceName(server)
	configFile := pm.configFile(server)

	// Ensure services directory exists
	if _, err := os.Stat(shawlServicesConfigPath); errors.Is(err, os.ErrNotExist) {
		_, _ = out.Write([]byte("Creating directory " + shawlServicesConfigPath + "\n"))
		if err := os.MkdirAll(shawlServicesConfigPath, 0755); err != nil {
			return false, errors.WithMessage(err, "failed to create services directory")
		}
	}

	// Build the new service configuration
	serviceConfig, err := pm.buildServiceConfig(server)
	if err != nil {
		return false, errors.WithMessage(err, "failed to build service config")
	}

	// Check if config file exists and compare
	configExists := false
	if _, err := os.Stat(configFile); err == nil {
		configExists = true
		oldConfig, err := os.ReadFile(configFile)
		if err != nil {
			return false, errors.WithMessage(err, "failed to read existing config")
		}

		if string(oldConfig) == serviceConfig {
			_, _ = out.Write([]byte("Service configuration unchanged\n"))
			return false, nil
		}

		_, _ = out.Write([]byte("Service configuration changed, recreating service\n"))

		// Delete existing service before recreating
		_, _ = pm.executor.ExecWithWriter(
			ctx,
			fmt.Sprintf("sc stop %s", serviceName),
			out,
			contracts.ExecutorOptions{WorkDir: pm.cfg.WorkDir()},
		)
		_, _ = pm.executor.ExecWithWriter(
			ctx,
			fmt.Sprintf("sc delete %s", serviceName),
			out,
			contracts.ExecutorOptions{WorkDir: pm.cfg.WorkDir()},
		)
	}

	// Find shawl executable
	shawlPath, err := exec.LookPath("shawl")
	if err != nil {
		return false, errors.WithMessage(err, "failed to find shawl executable in PATH")
	}

	// Build shawl arguments
	shawlArgs, err := pm.buildShawlArgs(server)
	if err != nil {
		return false, errors.WithMessage(err, "failed to build shawl arguments")
	}

	binPath := fmt.Sprintf("%s %s", shellquote.WindowsArgToString(shawlPath), strings.Join(shawlArgs, " "))

	// Create the service using sc create
	// Note: sc.exe requires binPath= to be followed by the value WITHOUT space,
	// and the entire value must be quoted if it contains spaces
	var scArgs string
	if pm.cfg.UseNetworkServiceUser {
		// Grant Modify permissions to NETWORK SERVICE for the server working directory
		workDir := server.WorkDir(pm.cfg)
		_, _ = out.Write([]byte("Granting permissions to NETWORK SERVICE for " + workDir + "\n"))
		if err := oscore.Grant(ctx, workDir, `NT AUTHORITY\NETWORK SERVICE`, oscore.GrantFlagModify); err != nil {
			return false, errors.WithMessage(err, "failed to grant permissions to NETWORK SERVICE")
		}

		// Using "NT AUTHORITY\NETWORK SERVICE" as the service account (no password required)
		scArgs = fmt.Sprintf(
			"sc create %s start=auto obj=%s binPath=%s",
			serviceName,
			shellquote.WindowsArgToString(`NT AUTHORITY\NETWORK SERVICE`),
			shellquote.WindowsArgToString(binPath),
		)
	} else {
		// Get user credentials from config
		rawPw, exists := pm.cfg.Users[server.User()]
		if !exists {
			return false, ErrUserNotFound
		}
		if rawPw == "" {
			return false, ErrInvalidUserPassword
		}

		var password string
		switch {
		case strings.HasPrefix(rawPw, "base64:"):
			pw, err := base64.StdEncoding.DecodeString(rawPw[7:])
			if err != nil {
				return false, errors.WithMessage(err, "failed to decode base64 password")
			}
			password = string(pw)
		default:
			password = rawPw
		}

		scArgs = fmt.Sprintf(
			"sc create %s start=auto obj=%s password=%s binPath=%s",
			serviceName,
			server.User(),
			shellquote.WindowsArgToString(password),
			shellquote.WindowsArgToString(binPath),
		)
	}

	_, _ = out.Write([]byte("Creating service " + serviceName + "\n"))
	_, _ = out.Write([]byte("Service configuration:\n"))
	_, _ = out.Write([]byte(serviceConfig))
	_, _ = out.Write([]byte("binPath: " + binPath + "\n"))

	result, err := pm.executor.ExecWithWriter(
		ctx,
		scArgs,
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	if err != nil {
		return false, errors.WithMessage(err, "failed to create service")
	}

	if result != 0 {
		return false, errors.New("sc create returned non-zero exit code")
	}

	// Save the configuration file
	if err := os.WriteFile(configFile, []byte(serviceConfig), 0644); err != nil {
		return false, errors.WithMessage(err, "failed to write config file")
	}

	return !configExists, nil
}

func (pm *Shawl) buildServiceConfig(server *domain.Server) (string, error) {
	cmd := domain.MakeFullCommand(
		pm.cfg,
		server,
		pm.cfg.Scripts.Start,
		server.StartCommand(),
	)

	if cmd == "" {
		return "", ErrEmptyCommand
	}

	// Build a simple config string for comparison purposes
	// Format: command|workdir|user
	serviceConfigContent := fmt.Sprintf(
		"command=%s\nworkdir=%s\nuser=%s\n",
		cmd,
		server.WorkDir(pm.cfg),
		server.User(),
	)

	return serviceConfigContent, nil
}

func (pm *Shawl) buildShawlArgs(server *domain.Server) ([]string, error) {
	serviceName := pm.serviceName(server)

	cmd := domain.MakeFullCommand(
		pm.cfg,
		server,
		pm.cfg.Scripts.Start,
		server.StartCommand(),
	)

	if cmd == "" {
		return nil, ErrEmptyCommand
	}

	cmdArr, err := shellquote.Split(cmd)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to split command")
	}

	executable := cmdArr[0]
	var cmdArgs []string

	if filepath.Ext(executable) == ".bat" {
		executable = "cmd.exe"
		cmdArgs = append(cmdArgs, "/c", cmdArr[0])
		cmdArgs = append(cmdArgs, cmdArr[1:]...)
	} else {
		cmdArgs = cmdArr[1:]
	}

	args := []string{
		"run",
		"--name", serviceName,
		"--restart",
		"--stop-timeout", shawlStopTimeout,
		"--cwd", server.WorkDir(pm.cfg),
		"--log-dir", filepath.Join(shawlServicesConfigPath, "logs"),
		"--log-as", serviceName + ".log",
		"--log-rotate", shawlLogRotate,
		"--log-retain", shawlLogRetain,
		"--",
		executable,
	}

	args = append(args, cmdArgs...)

	return args, nil
}

func (pm *Shawl) serviceName(server *domain.Server) string {
	return shawlServicePrefix + strconv.Itoa(server.ID())
}

func (pm *Shawl) configFile(server *domain.Server) string {
	return filepath.Join(shawlServicesConfigPath, pm.serviceName(server)+".yaml")
}

func (pm *Shawl) logPath(server *domain.Server) string {
	return filepath.Join(shawlServicesConfigPath, "logs", pm.serviceName(server)+".log_rCURRENT.log")
}

func (pm *Shawl) serviceExists(ctx context.Context, server *domain.Server) bool {
	serviceName := pm.serviceName(server)

	result, _ := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("sc query %s", serviceName),
		io.Discard,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)

	return result == 0
}
