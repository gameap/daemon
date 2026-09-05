//go:build linux

package processmanager

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	systemdFilesDir        = ".systemd-services"
	systemdServicesDir     = "/etc/systemd/system"
	systemdUserUnitSubpath = ".config/systemd/user"
	servicePrefix          = "gameap-server-"

	scopeSystem = "system"
	scopeUser   = "user"

	systemTarget = "multi-user.target"
	userTarget   = "default.target"

	outputSizeLimit = 30000

	stopTickerInterval = 500 * time.Millisecond
	stopTimeout        = 1 * time.Minute

	// Restart pacing written into generated units. Seconds, because that is the
	// unit systemd assumes for a bare number.
	unitRestartSec         = 5
	unitStartLimitInterval = 300
	unitStartLimitBurst    = 20
)

type SystemD struct {
	cfg      *config.Config
	executor contracts.Executor

	scope          string
	servicesDir    string
	runtimeEnv     map[string]string
	daemonUsername string

	lingerOnce sync.Once

	cpuSamplesMu sync.Mutex
	cpuSamples   map[string]systemdCPUSample
}

func NewSystemD(cfg *config.Config, _, detailedExecutor contracts.Executor) *SystemD {
	pm := &SystemD{
		cfg:         cfg,
		executor:    detailedExecutor,
		scope:       scopeSystem,
		servicesDir: systemdServicesDir,
		cpuSamples:  make(map[string]systemdCPUSample),
	}

	if cfg != nil && cfg.ProcessManager.Config["scope"] == scopeUser {
		pm.scope = scopeUser
		pm.initUserScope()
	}

	return pm
}

func (pm *SystemD) initUserScope() {
	cur, err := user.Current()
	if err != nil {
		logger.Warn(context.Background(), errors.WithMessage(err, "failed to resolve current user for systemd user scope"))
		return
	}

	pm.daemonUsername = cur.Username
	pm.servicesDir = filepath.Join(cur.HomeDir, systemdUserUnitSubpath)
	if err := os.MkdirAll(pm.servicesDir, 0o755); err != nil {
		logger.Warn(
			context.Background(),
			errors.WithMessagef(err, "failed to create systemd user unit dir %s", pm.servicesDir),
		)
	}

	pm.runtimeEnv = resolveSystemdUserEnv(cur)
}

func resolveSystemdUserEnv(cur *user.User) map[string]string {
	env := map[string]string{}

	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		if _, err := os.Stat(v); err == nil { //nolint:gosec // user-owned runtime dir from env
			env["XDG_RUNTIME_DIR"] = v
		}
	}

	if _, ok := env["XDG_RUNTIME_DIR"]; !ok {
		candidate := fmt.Sprintf("/run/user/%s", cur.Uid)
		if _, err := os.Stat(candidate); err == nil {
			env["XDG_RUNTIME_DIR"] = candidate
		} else {
			logger.Warn(
				context.Background(),
				errors.WithMessagef(
					err,
					"XDG_RUNTIME_DIR not resolved (tried env + %s); systemctl --user may fail",
					candidate,
				),
			)
		}
	}

	if v := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); v != "" {
		env["DBUS_SESSION_BUS_ADDRESS"] = v
	} else if runtimeDir, ok := env["XDG_RUNTIME_DIR"]; ok {
		env["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + filepath.Join(runtimeDir, "bus")
	}

	return env
}

func (pm *SystemD) isUserScope() bool {
	return pm.scope == scopeUser
}

func (pm *SystemD) systemctl(action, target string) string {
	prefix := "systemctl"
	if pm.isUserScope() {
		prefix = "systemctl --user"
	}

	if target == "" {
		return prefix + " " + action
	}
	return prefix + " " + action + " " + target
}

func (pm *SystemD) execOpts() contracts.ExecutorOptions {
	opts := contracts.ExecutorOptions{
		WorkDir: pm.cfg.WorkDir(),
	}
	if len(pm.runtimeEnv) > 0 {
		opts.Env = pm.runtimeEnv
	}
	return opts
}

func (pm *SystemD) requireUserMatch(server *domain.Server) error {
	if !pm.isUserScope() {
		return nil
	}

	requested := server.User()
	if requested == "" || requested == pm.daemonUsername {
		return nil
	}

	return errors.WithMessagef(
		ErrUserMismatch,
		"server requests user %q but daemon runs as %q",
		requested, pm.daemonUsername,
	)
}

func (pm *SystemD) ensureLingerChecked(ctx context.Context, out io.Writer) {
	if !pm.isUserScope() {
		return
	}

	pm.lingerOnce.Do(func() {
		if pm.daemonUsername == "" {
			return
		}

		cmd := fmt.Sprintf("loginctl show-user %s --property=Linger --value", pm.daemonUsername)
		output, code, err := pm.executor.Exec(ctx, cmd, pm.execOpts())
		if err != nil {
			logger.WithError(ctx, err).Warn("failed to check linger state for systemd user scope")
			return
		}
		if code != 0 {
			return
		}

		if strings.TrimSpace(string(output)) != "yes" {
			msg := fmt.Sprintf(
				"linger is disabled for user %q; user services will be killed at logout. "+
					"Enable with: sudo loginctl enable-linger %s",
				pm.daemonUsername, pm.daemonUsername,
			)
			logger.Logger(ctx).Warn(msg)
			if out != nil {
				_, _ = out.Write([]byte(msg + "\n"))
			}
		}
	})
}

func (pm *SystemD) Install(
	_ context.Context, server *domain.Server, _ io.Writer,
) (domain.Result, error) {
	if err := pm.requireUserMatch(server); err != nil {
		return domain.ErrorResult, err
	}

	return domain.SuccessResult, nil
}

func (pm *SystemD) Uninstall(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	if err := pm.requireUserMatch(server); err != nil {
		return domain.ErrorResult, err
	}
	pm.ensureLingerChecked(ctx, out)

	resolvedServiceName := pm.resolveServiceName(server)

	s, err := pm.status(ctx, resolvedServiceName, out)
	if err != nil {
		_, _ = out.Write([]byte("Failed to get service status: " + err.Error() + "\n"))
		logger.WithError(ctx, err).Warn("failed to get service status")
	} else if s == domain.SuccessResult {
		_, _ = out.Write([]byte("Service " + resolvedServiceName + " is running, stopping it first\n"))

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

	// Remove both XID-based and legacy UUID-based files
	for _, socketFile := range []string{pm.socketFile(server), pm.legacySocketFile(server)} {
		_, _ = out.Write([]byte("Removing socket file at " + socketFile + "\n"))
		if err := os.Remove(socketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			_, _ = out.Write([]byte("Failed to remove socket file: " + err.Error() + "\n"))
			logger.WithError(ctx, err).Warn("failed to remove socket file")
		}
	}

	for _, serviceFile := range []string{pm.serviceFile(server), pm.legacyServiceFile(server)} {
		_, _ = out.Write([]byte("Removing service file at " + serviceFile + "\n"))
		if err := os.Remove(serviceFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			_, _ = out.Write([]byte("Failed to remove service file: " + err.Error() + "\n"))
			logger.WithError(ctx, err).Warn("failed to remove service file")
		}
	}

	err = pm.daemonReload(ctx)
	if err != nil {
		_, _ = out.Write([]byte("Failed to daemon-reload: " + err.Error() + "\n"))
		logger.Logger(ctx).WithError(err).Warn("Failed to daemon-reload")
	}

	return domain.SuccessResult, nil
}

func (pm *SystemD) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.requireUserMatch(server); err != nil {
		return domain.ErrorResult, err
	}
	pm.ensureLingerChecked(ctx, out)

	// Clean up legacy UUID-based service/socket files if they differ from new XID-based names
	if pm.legacyServiceFile(server) != pm.serviceFile(server) {
		_ = os.Remove(pm.legacyServiceFile(server))
		_ = os.Remove(pm.legacySocketFile(server))
	}

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
	if err := pm.requireUserMatch(server); err != nil {
		return domain.ErrorResult, err
	}
	pm.ensureLingerChecked(ctx, out)

	socketName := pm.resolveSocketName(server)
	serviceName := pm.resolveServiceName(server)

	_, err := pm.executor.ExecWithWriter(
		ctx,
		pm.systemctl("stop", socketName),
		out,
		pm.execOpts(),
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		pm.systemctl("stop", serviceName),
		out,
		pm.execOpts(),
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	if result != 0 {
		return domain.Result(result), nil
	}

	_, _ = out.Write([]byte("Waiting for service to stop...\n"))
	if err := pm.waitForServiceStopped(ctx, server, serviceName, out); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to wait for service to stop")
	}

	_, _ = out.Write([]byte("Service stopped\n"))
	return domain.SuccessResult, nil
}

func (pm *SystemD) waitForServiceStopped(
	ctx context.Context, _ *domain.Server, serviceName string, out io.Writer,
) error {
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
			status, _ := pm.status(ctx, serviceName, out)
			if status == domain.ErrorResult {
				// Service is not running (stopped)
				return nil
			}
		}
	}
}

func (pm *SystemD) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if err := pm.requireUserMatch(server); err != nil {
		return domain.ErrorResult, err
	}
	pm.ensureLingerChecked(ctx, out)

	return pm.command(ctx, server, "restart", out)
}

func (pm *SystemD) command(
	ctx context.Context, server *domain.Server, command string, out io.Writer,
) (domain.Result, error) {
	// Stop and remove legacy UUID-based service/socket if they differ from new XID-based names
	if pm.legacyServiceFile(server) != pm.serviceFile(server) {
		legacyServiceName := pm.legacyServiceName(server)
		legacySocketName := pm.legacySocketName(server)
		if _, err := os.Stat(pm.legacyServiceFile(server)); err == nil {
			_, _ = pm.executor.ExecWithWriter(ctx, pm.systemctl("stop", legacySocketName), out, pm.execOpts())
			_, _ = pm.executor.ExecWithWriter(ctx, pm.systemctl("stop", legacyServiceName), out, pm.execOpts())
			_ = os.Remove(pm.legacyServiceFile(server))
			_ = os.Remove(pm.legacySocketFile(server))
		}
	}

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
			pm.systemctl("start", socketName),
			out,
			pm.execOpts(),
		)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
		}
	}

	// A unit that exhausted its start limit stays in failed, and systemd then
	// refuses every further start with "start request repeated too quickly"
	// until the failure is cleared. Clearing it here is what lets both the
	// operator and the servers loop start a server that crash-looped earlier.
	// reset-failed on a healthy unit is a no-op.
	_, _, err = pm.executor.Exec(ctx, pm.systemctl("reset-failed", serviceName), pm.execOpts())
	if err != nil {
		logger.WithError(ctx, err).Debug("failed to reset unit failure state")
	}

	result, err := pm.executor.ExecWithWriter(
		ctx,
		pm.systemctl(command, serviceName),
		out,
		pm.execOpts(),
	)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	return domain.Result(result), nil
}

func (pm *SystemD) daemonReload(ctx context.Context) error {
	_, _, err := pm.executor.Exec(
		ctx,
		pm.systemctl("daemon-reload", ""),
		pm.execOpts(),
	)
	return err
}

func (pm *SystemD) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	return pm.status(ctx, pm.resolveServiceName(server), out)
}

// status reports whether the unit is up.
//
// The unit state is read with `systemctl show` rather than from the exit code of
// `systemctl status`. Units generated for autostarting servers carry
// Restart=always, and between a crash and the next start systemd holds them in
// activating/auto-restart. That state is indistinguishable from a stopped unit
// by exit code alone, so the daemon would report the server offline and race
// systemd to start it. Reading ActiveState keeps the two supervisors from
// fighting: while systemd is bringing the unit back the server counts as up,
// and the daemon steps in only once systemd has given up and left the unit
// inactive or failed.
func (pm *SystemD) status(ctx context.Context, name string, out io.Writer) (domain.Result, error) {
	cmd := pm.systemctl("show", name) + " --property=LoadState,ActiveState,SubState,Result"

	output, code, err := pm.executor.Exec(ctx, cmd, pm.execOpts())
	if err != nil {
		return domain.UnknownResult, errors.WithMessage(err, "failed to exec command")
	}
	if code != 0 {
		return domain.UnknownResult, errors.WithMessagef(
			ErrStatusUndetermined, "systemctl show exited with code %d", code,
		)
	}

	props := parseSystemctlProperties(output)

	_, _ = out.Write([]byte(fmt.Sprintf(
		"Unit %s: load=%s active=%s sub=%s result=%s\n",
		name, props["LoadState"], props["ActiveState"], props["SubState"], props["Result"],
	)))

	if props["LoadState"] == "not-found" {
		return domain.ErrorResult, nil
	}

	switch props["ActiveState"] {
	case "active", "activating", "reloading", "deactivating":
		return domain.SuccessResult, nil
	case "inactive", "failed":
		return domain.ErrorResult, nil
	case "":
		return domain.UnknownResult, errors.WithMessagef(
			ErrStatusUndetermined, "systemctl show reported no ActiveState for %s", name,
		)
	}

	return domain.UnknownResult, errors.WithMessagef(
		ErrStatusUndetermined, "unknown ActiveState %q for %s", props["ActiveState"], name,
	)
}

func parseSystemctlProperties(raw []byte) map[string]string {
	props := make(map[string]string, 4)

	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}

		props[line[:eq]] = line[eq+1:]
	}

	return props
}

func (pm *SystemD) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	f, err := os.Open(pm.resolveLogFile(server))
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
	f, err := os.OpenFile(pm.resolveStdinFile(server), os.O_WRONLY, 0)
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

// makeService writes the unit file for the server.
//
// The content is built before anything on disk is touched, and it lands through
// a temporary file in the same directory followed by a rename. Truncating the
// unit first would leave a zero-byte file behind whenever building the content
// fails — on a missing game binary, say — and because daemon-reload is global,
// the next start of any other server would load that empty unit.
func (pm *SystemD) makeService(_ context.Context, server *domain.Server, out io.Writer) error {
	c, err := pm.buildServiceConfig(server)
	if err != nil {
		return errors.WithMessage(err, "failed to build service config")
	}

	serviceFile := pm.serviceFile(server)

	_, _ = out.Write([]byte("Creating service file at " + serviceFile + "\n"))
	_, _ = out.Write([]byte("----- BEGIN SERVICE FILE -----\n"))
	_, _ = out.Write([]byte(c + "\n"))
	_, _ = out.Write([]byte("----- END SERVICE FILE -----\n\n\n"))

	if err := writeFileAtomic(serviceFile, []byte(c), 0644); err != nil {
		return errors.WithMessagef(err, "failed to write service file %s", serviceFile)
	}

	return nil
}

func writeFileAtomic(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return errors.Wrapf(err, "failed to create temporary file in %s", dir)
	}
	tmpName := f.Name()

	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := f.Write(content); err != nil {
		return errors.Wrapf(err, "failed to write temporary file %s", tmpName)
	}
	if err := f.Chmod(perm); err != nil {
		return errors.Wrapf(err, "failed to set permissions on %s", tmpName)
	}
	if err := f.Sync(); err != nil {
		return errors.Wrapf(err, "failed to flush %s", tmpName)
	}
	if err := f.Close(); err != nil {
		return errors.Wrapf(err, "failed to close %s", tmpName)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errors.Wrapf(err, "failed to move %s into place", tmpName)
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
	builder.WriteString(server.XID())
	builder.WriteString(")\n")

	builder.WriteString("After=network.target\n")

	builder.WriteString("Wants=network-online.target systemd-networkd-wait-online.service\n")

	// The start rate limit belongs to the unit, not the service: systemd ignores
	// these two keys in [Service] and only warns about them in the journal.
	//
	// It is widened well past the default of 5 starts per 10s. A game server
	// that crashes on a bad map or a bad config trips that default within
	// seconds, after which the unit sits in failed and refuses every further
	// start until something runs reset-failed.
	if server.AutoStartSetting() {
		builder.WriteString("StartLimitIntervalSec=" + strconv.Itoa(unitStartLimitInterval) + "\n")
		builder.WriteString("StartLimitBurst=" + strconv.Itoa(unitStartLimitBurst) + "\n")
	}

	builder.WriteString("\n")

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
	builder.WriteString(server.XID())
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

	// Supervision follows the server's own autostart preference: a server the
	// operator does not want brought back must stay down after a crash. The
	// persistent setting is read rather than AutoStart(), which also reflects
	// autostart_current and is 0 for the whole duration of a deliberate stop —
	// reading it here would strip supervision from the unit on every stop.
	//
	// The matching start limit is written into [Unit] above.
	if server.AutoStartSetting() {
		builder.WriteString("Restart=always\n")
		builder.WriteString("RestartSec=" + strconv.Itoa(unitRestartSec) + "\n")
	} else {
		builder.WriteString("Restart=no\n")
	}

	// Enable cgroup accounting so the daemon can read CPU/memory/IO/IP/tasks
	// counters via `systemctl show` for metrics. Without these directives
	// systemd reports `[not set]` (or UINT64_MAX) for the corresponding
	// properties.
	builder.WriteString("CPUAccounting=yes\n")
	builder.WriteString("MemoryAccounting=yes\n")
	builder.WriteString("TasksAccounting=yes\n")
	builder.WriteString("IOAccounting=yes\n")
	builder.WriteString("IPAccounting=yes\n")

	if !pm.isUserScope() {
		runAsUser, group, err := pm.userAndGroup(server)
		if err != nil {
			return "", errors.WithMessage(err, "failed to get user")
		}

		builder.WriteString("User=")
		builder.WriteString(runAsUser)
		builder.WriteString("\n")

		builder.WriteString("Group=")
		builder.WriteString(group)
		builder.WriteString("\n")
	}

	// Resource limits
	if server.RAMLimit() > 0 {
		builder.WriteString("MemoryMax=")
		builder.WriteString(strconv.FormatInt(server.RAMLimit(), 10))
		builder.WriteString("\n")
	}

	if server.CPULimit() > 0 {
		// Convert millicores to CPUQuota percentage
		// 1000 millicores = 100% (1 core)
		cpuQuota := server.CPULimit() / 10
		builder.WriteString("CPUQuota=")
		builder.WriteString(strconv.Itoa(cpuQuota))
		builder.WriteString("%\n")
	}

	// Environment variables
	for key, value := range server.EnvironmentVars() {
		builder.WriteString("Environment=")
		builder.WriteString(escapeSystemdEnv(key, value))
		builder.WriteString("\n")
	}

	builder.WriteString("\n")

	// [Install]
	builder.WriteString("[Install]\n")

	builder.WriteString("WantedBy=")
	builder.WriteString(pm.installTarget())
	builder.WriteString("\n")

	return builder.String(), nil
}

func (pm *SystemD) installTarget() string {
	if pm.isUserScope() {
		return userTarget
	}
	return systemTarget
}

func (pm *SystemD) makeStartCommand(server *domain.Server) (string, error) {
	args, err := domain.BuildCommandArgs(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand())
	if err != nil {
		return "", errors.WithMessage(err, "failed to build command")
	}

	if len(args) == 0 {
		return "", ErrEmptyCommand
	}

	cmd := args[0]

	var foundPath string

	if filepath.IsAbs(cmd) {
		foundPath, err = exec.LookPath(cmd)
		if err != nil {
			return "", errors.WithMessagef(err, "failed to find command '%s'", cmd)
		}
	} else {
		foundPath, err = exec.LookPath(filepath.Join(server.WorkDir(pm.cfg), cmd))
		if err != nil {
			foundPath, err = exec.LookPath(cmd)
			if err != nil {
				return "", errors.WithMessagef(err, "failed to find command '%s'", cmd)
			}
		}
	}

	args[0] = foundPath

	return systemdQuoteArgs(args), nil
}

// systemdQuoteArgs serializes an argument vector for a systemd ExecStart= line.
// systemd's parser is not a POSIX shell: it performs environment ("$", "${}")
// and specifier ("%") expansion even inside quotes, so those are escaped as "$$"
// and "%%", and each argument is double-quoted when it contains whitespace or
// quoting characters so it stays a single argument.
func systemdQuoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = systemdQuoteArg(arg)
	}

	return strings.Join(quoted, " ")
}

func systemdQuoteArg(arg string) string {
	arg = strings.NewReplacer("%", "%%", "$", "$$").Replace(arg)

	if arg == "" {
		return `""`
	}

	if !strings.ContainsAny(arg, " \t\n'\"\\") {
		return arg
	}

	var b strings.Builder
	b.Grow(len(arg) + 2)
	b.WriteByte('"')

	for i := 0; i < len(arg); i++ {
		switch arg[i] {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(arg[i])
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(arg[i])
		}
	}

	b.WriteByte('"')

	return b.String()
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
	builder.WriteString(server.XID())
	builder.WriteString(")\n\n")

	// [Socket]
	builder.WriteString("[Socket]\n")

	builder.WriteString("ListenFIFO=")
	builder.WriteString(pm.stdinFile(server))
	builder.WriteString("\n")

	builder.WriteString("Service=")
	builder.WriteString(servicePrefix)
	builder.WriteString(server.XID())
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
	builder.WriteString(server.XID())
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
	builder.WriteString(server.XID())
	builder.WriteString(".stdin")

	return builder.String()
}

func (pm *SystemD) serviceName(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(50)

	builder.WriteString(servicePrefix)
	builder.WriteString(server.XID())
	builder.WriteString(".service")

	return builder.String()
}

func (pm *SystemD) serviceFile(server *domain.Server) string {
	return filepath.Join(pm.servicesDir, pm.serviceName(server))
}

func (pm *SystemD) socketName(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(50)

	builder.WriteString(servicePrefix)
	builder.WriteString(server.XID())
	builder.WriteString(".socket")

	return builder.String()
}

func (pm *SystemD) socketFile(server *domain.Server) string {
	return filepath.Join(pm.servicesDir, pm.socketName(server))
}

func (pm *SystemD) legacyServiceName(server *domain.Server) string {
	return servicePrefix + server.UUID() + ".service"
}

func (pm *SystemD) legacySocketName(server *domain.Server) string {
	return servicePrefix + server.UUID() + ".socket"
}

func (pm *SystemD) legacyServiceFile(server *domain.Server) string {
	return filepath.Join(pm.servicesDir, pm.legacyServiceName(server))
}

func (pm *SystemD) legacySocketFile(server *domain.Server) string {
	return filepath.Join(pm.servicesDir, pm.legacySocketName(server))
}

func (pm *SystemD) legacyLogFile(server *domain.Server) string {
	return filepath.Join(pm.cfg.WorkDir(), systemdFilesDir, server.UUID()+".log")
}

func (pm *SystemD) legacyStdinFile(server *domain.Server) string {
	return filepath.Join(pm.cfg.WorkDir(), systemdFilesDir, server.UUID()+".stdin")
}

// resolveServiceName returns the name of the existing systemd service for this server.
// Checks XID-based name first, then falls back to legacy UUID-based name.
func (pm *SystemD) resolveServiceName(server *domain.Server) string {
	name := pm.serviceName(server)
	if _, err := os.Stat(pm.serviceFile(server)); err == nil {
		return name
	}

	legacyName := pm.legacyServiceName(server)
	if _, err := os.Stat(pm.legacyServiceFile(server)); err == nil {
		return legacyName
	}

	return name
}

func (pm *SystemD) resolveSocketName(server *domain.Server) string {
	name := pm.socketName(server)
	if _, err := os.Stat(pm.socketFile(server)); err == nil {
		return name
	}

	legacyName := pm.legacySocketName(server)
	if _, err := os.Stat(pm.legacySocketFile(server)); err == nil {
		return legacyName
	}

	return name
}

func (pm *SystemD) resolveLogFile(server *domain.Server) string {
	logFile := pm.logFile(server)
	if _, err := os.Stat(logFile); err == nil {
		return logFile
	}

	legacyLogFile := pm.legacyLogFile(server)
	if _, err := os.Stat(legacyLogFile); err == nil {
		return legacyLogFile
	}

	return logFile
}

func (pm *SystemD) resolveStdinFile(server *domain.Server) string {
	stdinFile := pm.stdinFile(server)
	if _, err := os.Stat(stdinFile); err == nil {
		return stdinFile
	}

	legacyStdinFile := pm.legacyStdinFile(server)
	if _, err := os.Stat(legacyStdinFile); err == nil {
		return legacyStdinFile
	}

	return stdinFile
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

func (pm *SystemD) Attach(
	ctx context.Context, server *domain.Server, in io.Reader, out io.Writer,
) error {
	serviceName := pm.resolveServiceName(server)

	status, err := pm.status(ctx, serviceName, io.Discard)
	if err != nil {
		return errors.WithMessage(err, "failed to check service status")
	}
	if status != domain.SuccessResult {
		return ErrServiceNotRunning
	}

	logFile, err := os.Open(pm.resolveLogFile(server))
	if err != nil {
		return errors.WithMessage(err, "failed to open log file")
	}
	if _, err := logFile.Seek(0, io.SeekEnd); err != nil {
		_ = logFile.Close()
		return errors.WithMessage(err, "failed to seek log file")
	}

	stdinFile, err := pm.openFIFOWithTimeout(ctx, pm.resolveStdinFile(server), 5*time.Second)
	if err != nil {
		_ = logFile.Close()
		return err
	}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-gctx.Done()
		_ = stdinFile.Close()
		_ = logFile.Close()
		return nil
	})

	g.Go(func() error {
		buf := make([]byte, 4096)
		for {
			n, readErr := in.Read(buf)
			if n > 0 {
				if _, writeErr := stdinFile.Write(buf[:n]); writeErr != nil {
					if errors.Is(writeErr, os.ErrClosed) {
						return nil
					}
					return errors.WithMessage(writeErr, "failed to write to stdin FIFO")
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrClosedPipe) {
					return nil
				}
				return errors.WithMessage(readErr, "stdin read failed")
			}
		}
	})

	g.Go(func() error {
		buf := make([]byte, 4096)
		for {
			n, readErr := logFile.Read(buf)
			if n > 0 {
				if _, writeErr := out.Write(buf[:n]); writeErr != nil {
					return errors.WithMessage(writeErr, "failed to write output")
				}
			}
			if readErr != nil {
				if errors.Is(readErr, os.ErrClosed) {
					return nil
				}
				if readErr == io.EOF {
					select {
					case <-gctx.Done():
						return nil
					case <-time.After(200 * time.Millisecond):
						continue
					}
				}
				return errors.WithMessage(readErr, "failed to read log file")
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
				s, _ := pm.status(gctx, serviceName, io.Discard)
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

func (pm *SystemD) openFIFOWithTimeout(
	ctx context.Context, path string, timeout time.Duration,
) (*os.File, error) {
	type result struct {
		file *os.File
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		ch <- result{f, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, errors.WithMessage(r.err, "failed to open stdin FIFO")
		}
		return r.file, nil
	case <-time.After(timeout):
		return nil, errors.New("timeout opening stdin FIFO: service may not be running")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (pm *SystemD) HasOwnInstallation(_ *domain.Server) bool {
	return false
}

// escapeSystemdEnv formats and escapes an environment variable for systemd.
// Systemd requires proper quoting and escaping of special characters.
// Format: "KEY=value" with proper escaping of quotes, backslashes, and special chars.
func escapeSystemdEnv(key, value string) string {
	var sb strings.Builder
	sb.Grow(len(key) + len(value) + 10)

	sb.WriteByte('"')
	sb.WriteString(key)
	sb.WriteByte('=')

	// Escape special characters in value
	for _, r := range value {
		switch r {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\t':
			sb.WriteString("\\t")
		case '%':
			sb.WriteString("%%")
		default:
			sb.WriteRune(r)
		}
	}

	sb.WriteByte('"')
	return sb.String()
}
