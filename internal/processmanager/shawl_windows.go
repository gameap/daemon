//go:build windows

package processmanager

import (
	"context"
	"encoding/base64"
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
	"github.com/gameap/gameapctl/pkg/oscore"
	"github.com/pkg/errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const (
	shawlOutputSizeLimit = 30000
	shawlLogTailLines    = 40

	stateTickerInterval = 500 * time.Millisecond
	stopTimeout         = 1 * time.Minute
	startTimeout        = 30 * time.Second
	deleteTimeout       = 15 * time.Second
)

type Shawl struct {
	cfg *config.Config
}

// NewShawl builds the Windows process manager. It drives the service control manager through
// its API rather than sc.exe, so it needs no executor.
func NewShawl(cfg *config.Config, _, _ contracts.Executor) *Shawl {
	return &Shawl{cfg: cfg}
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

	_, _ = pm.Stop(ctx, server, out)

	_, _ = out.Write([]byte("Deleting service " + serviceName + "\n"))

	err := deleteService(serviceName)
	if err != nil && !errors.Is(err, ErrServiceNotFound) {
		writeServiceError(out, err, pm.accountHintFor(server), pm.logDir())

		return domain.ErrorResult, errors.WithMessage(err, "failed to delete service")
	}

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

	if _, err := pm.makeService(ctx, server, out); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	serviceName := pm.serviceName(server)
	_, _ = out.Write([]byte("Starting service " + serviceName + "\n"))

	err := startService(serviceName)

	if isServiceErrno(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		_, _ = out.Write([]byte("Service is already running\n"))

		return domain.SuccessResult, nil
	}

	// A password that changed in the daemon config alone does not change the service
	// fingerprint, so the service still carries the old credentials. Registering it again with
	// the current ones is the only way to tell a rotated password from a wrong one.
	if isServiceErrno(err, windows.ERROR_SERVICE_LOGON_FAILED) {
		_, _ = out.Write([]byte("Service credentials were rejected, registering the service again\n"))

		if recreateErr := pm.recreateService(ctx, server, out); recreateErr != nil {
			return domain.ErrorResult, errors.WithMessage(recreateErr, "failed to recreate service")
		}

		err = startService(serviceName)
	}

	if err != nil {
		writeServiceError(out, err, pm.accountHintFor(server), pm.logDir())

		return domain.ErrorResult, errors.WithMessage(err, "failed to start service")
	}

	if err := pm.waitForServiceRunning(ctx, server, out); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to wait for service to start")
	}

	return domain.SuccessResult, nil
}

func (pm *Shawl) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	serviceName := pm.serviceName(server)
	_, _ = out.Write([]byte("Stopping service " + serviceName + "\n"))

	err := stopService(serviceName)

	switch {
	case errors.Is(err, ErrServiceNotFound):
		_, _ = out.Write([]byte("Service is not registered\n"))

		return domain.SuccessResult, nil
	case isServiceErrno(err, windows.ERROR_SERVICE_NOT_ACTIVE):
		_, _ = out.Write([]byte("Service is already stopped\n"))

		return domain.SuccessResult, nil
	case err != nil:
		writeServiceError(out, err, pm.accountHintFor(server), pm.logDir())

		return domain.ErrorResult, errors.WithMessage(err, "failed to stop service")
	}

	_, _ = out.Write([]byte("Waiting for service to stop...\n"))

	if err := pm.waitForServiceState(ctx, serviceName, svc.Stopped, stopTimeout); err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to wait for service to stop")
	}

	_, _ = out.Write([]byte("Service stopped\n"))

	return domain.SuccessResult, nil
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

	status, err := queryService(serviceName)
	if errors.Is(err, ErrServiceNotFound) {
		logger.Debug(ctx, "Service "+serviceName+" is not registered")
		_, _ = out.Write([]byte("Service " + serviceName + " is not registered\n"))

		return domain.ErrorResult, nil
	}
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to query service status")
	}

	_, _ = out.Write([]byte(
		"Service " + serviceName + " state: " + serviceStateName(uint32(status.State)) + "\n",
	))

	if status.State == svc.Running {
		return domain.SuccessResult, nil
	}

	return domain.ErrorResult, nil
}

func (pm *Shawl) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	logFile := pm.logPath(server)

	f, err := os.Open(logFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = out.Write([]byte("Log file " + logFile + " does not exist\n"))

			return domain.SuccessResult, nil
		}

		return domain.ErrorResult, errors.Wrap(err, "failed to open log file")
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.Warn(ctx, errors.Wrap(err, "failed to close log file"))
		}
	}()

	truncated, err := seekLogTail(f)
	if err != nil {
		return domain.ErrorResult, err
	}

	scanner := newShawlLogScanner(f)

	if truncated {
		scanner.Scan()
	}

	for scanner.Scan() {
		msg := parseShawlLogLine(scanner.Text())
		if msg != "" {
			_, _ = out.Write([]byte(msg + "\n"))
		}
	}

	if err := scanner.Err(); err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to read log file")
	}

	return domain.SuccessResult, nil
}

func (pm *Shawl) SendInput(
	_ context.Context, _ string, _ *domain.Server, _ io.Writer,
) (domain.Result, error) {
	return domain.ErrorResult, errors.New("input is not supported on Windows")
}

// shawlServicePlan is the service a server should have: everything needed to register it, plus
// the marker file that records it.
type shawlServicePlan struct {
	serviceName string
	account     string
	password    string
	executable  string
	arguments   []string
	binaryPath  string
	config      string
}

func (pm *Shawl) buildServicePlan(server *domain.Server) (shawlServicePlan, error) {
	serviceName := pm.serviceName(server)

	shawlPath, err := exec.LookPath("shawl")
	if err != nil {
		return shawlServicePlan{}, errors.Wrap(err, "failed to find shawl executable in PATH")
	}

	cmdArr, err := domain.BuildCommandArgs(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand())
	if err != nil {
		return shawlServicePlan{}, errors.WithMessage(err, "failed to build command")
	}

	arguments, err := buildShawlRunArgs(serviceName, server.WorkDir(pm.cfg), pm.logDir(), cmdArr)
	if err != nil {
		return shawlServicePlan{}, errors.WithMessage(err, "failed to build shawl arguments")
	}

	command := domain.MakeFullCommand(pm.cfg, server, pm.cfg.Scripts.Start, server.StartCommand())
	if command == "" {
		return shawlServicePlan{}, ErrEmptyCommand
	}

	account := oscore.WindowsNetworkServiceAccount
	password := ""

	if !pm.cfg.UseNetworkServiceUser {
		account = server.User()

		password, err = pm.userPassword(server)
		if err != nil {
			return shawlServicePlan{}, err
		}
	}

	plan := shawlServicePlan{
		serviceName: serviceName,
		account:     oscore.NormalizeWindowsServiceAccount(account),
		password:    password,
		executable:  shawlPath,
		arguments:   arguments,
		binaryPath:  expectedBinaryPathName(shawlPath, arguments),
	}

	plan.config = shawlServiceFingerprint{
		ServiceName:        serviceName,
		Account:            plan.account,
		NetworkServiceUser: pm.cfg.UseNetworkServiceUser,
		WorkDir:            server.WorkDir(pm.cfg),
		BinaryPathName:     plan.binaryPath,
		Command:            command,
	}.String()

	return plan, nil
}

func (pm *Shawl) userPassword(server *domain.Server) (string, error) {
	rawPw, exists := pm.cfg.Users[server.User()]
	if !exists {
		return "", ErrUserNotFound
	}

	if rawPw == "" {
		return "", ErrInvalidUserPassword
	}

	if after, found := strings.CutPrefix(rawPw, "base64:"); found {
		pw, err := base64.StdEncoding.DecodeString(after)
		if err != nil {
			return "", errors.Wrap(err, "failed to decode base64 password")
		}

		return string(pw), nil
	}

	return rawPw, nil
}

// makeService registers the service if it is missing, and registers it again whenever the
// service the system actually has drifted from the one the config describes.
func (pm *Shawl) makeService(ctx context.Context, server *domain.Server, out io.Writer) (bool, error) {
	if err := pm.ensureDirs(out); err != nil {
		return false, err
	}

	plan, err := pm.buildServicePlan(server)
	if err != nil {
		return false, errors.WithMessage(err, "failed to build service config")
	}

	if err := pm.grantLogDirAccess(ctx, plan.account); err != nil {
		return false, err
	}

	installed, err := serviceInstalled(plan.serviceName)
	if err != nil {
		return false, errors.WithMessage(err, "failed to check whether the service is registered")
	}

	reason, err := pm.serviceDrift(plan, installed)
	if err != nil {
		return false, err
	}

	if reason == "" {
		// The registered service is already the one the config describes, so the marker file is
		// brought up to date on its own. Registering the service again would stop a running
		// game server for nothing.
		if err := pm.syncConfigFile(server, plan); err != nil {
			return false, err
		}

		_, _ = out.Write([]byte("Service configuration unchanged\n"))

		return false, nil
	}

	_, _ = out.Write([]byte("Registering service " + plan.serviceName + ": " + reason + "\n"))

	if err := pm.createServiceFromPlan(ctx, server, plan, installed, out); err != nil {
		return false, err
	}

	return !installed, nil
}

// serviceDrift explains why the service has to be registered again, or returns an empty string
// when the system already has the service the config describes.
//
// The registered service is the authority, never the marker file: the marker is what some
// earlier daemon wrote, and it says nothing about a service that was removed or reconfigured
// behind the daemon's back. The account and the command line together cover everything that
// makes a service the wrong one, so a stale marker beside a correct service is not a reason to
// stop a running game server.
func (pm *Shawl) serviceDrift(plan shawlServicePlan, installed bool) (string, error) {
	if !installed {
		return "service is not registered", nil
	}

	config, err := readServiceConfig(plan.serviceName)
	if errors.Is(err, ErrServiceNotFound) {
		return "service is not registered", nil
	}
	if err != nil {
		return "", errors.WithMessage(err, "failed to read service configuration")
	}

	if !sameServiceAccount(config.ServiceStartName, plan.account) {
		return "registered for account " + quoteName(config.ServiceStartName) +
			" instead of " + quoteName(plan.account), nil
	}

	if config.BinaryPathName != plan.binaryPath {
		return "registered with a different command line", nil
	}

	return "", nil
}

// syncConfigFile records the plan next to the service. The file is what an operator reads to
// see how a service was set up; the daemon itself compares against the service control manager.
func (pm *Shawl) syncConfigFile(server *domain.Server, plan shawlServicePlan) error {
	configFile := pm.configFile(server)

	stored, err := os.ReadFile(configFile)
	if err == nil && string(stored) == plan.config {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Wrap(err, "failed to read existing service config")
	}

	if err := os.WriteFile(configFile, []byte(plan.config), 0600); err != nil {
		return errors.Wrap(err, "failed to write config file")
	}

	return nil
}

func (pm *Shawl) createServiceFromPlan(
	ctx context.Context, server *domain.Server, plan shawlServicePlan, installed bool, out io.Writer,
) error {
	if installed {
		pm.removeService(ctx, plan.serviceName, out)
	}

	if err := pm.grantWorkDirAccess(ctx, server, plan.account, out); err != nil {
		return err
	}

	_, _ = out.Write([]byte(
		"Creating service " + plan.serviceName + " for account " + plan.account + "\n",
	))
	_, _ = out.Write([]byte("Service executable: " + plan.executable + "\n"))

	// The command line embeds the whole game command, which may carry credentials passed as
	// arguments, so it stays in the local daemon log. The account password never appears in it:
	// it is handed straight to the service control manager.
	logger.Debug(ctx, "Service command line: "+plan.binaryPath)

	err := createService(serviceSpec{
		Name:     plan.serviceName,
		ExePath:  plan.executable,
		Args:     plan.arguments,
		Account:  plan.account,
		Password: plan.password,
	})
	if err != nil {
		writeServiceError(out, err, plan.account, pm.logDir())

		return errors.WithMessage(err, "failed to create service")
	}

	return pm.syncConfigFile(server, plan)
}

// recreateService registers the service again from the current config, ignoring the marker file.
func (pm *Shawl) recreateService(ctx context.Context, server *domain.Server, out io.Writer) error {
	plan, err := pm.buildServicePlan(server)
	if err != nil {
		return errors.WithMessage(err, "failed to build service config")
	}

	installed, err := serviceInstalled(plan.serviceName)
	if err != nil {
		return errors.WithMessage(err, "failed to check whether the service is registered")
	}

	return pm.createServiceFromPlan(ctx, server, plan, installed, out)
}

// grantWorkDirAccess lets the service account reach the game server files. It walks the whole
// server directory, so it runs only when the service is registered.
func (pm *Shawl) grantWorkDirAccess(ctx context.Context, server *domain.Server, account string, out io.Writer) error {
	workDir := server.WorkDir(pm.cfg)

	_, _ = out.Write([]byte("Granting permissions to " + account + " for " + workDir + "\n"))

	if err := oscore.Grant(ctx, workDir, account, oscore.GrantFlagModify); err != nil {
		return errors.WithMessagef(err, "failed to grant permissions to %s for %s", account, workDir)
	}

	return nil
}

// grantLogDirAccess lets the service account write the shawl log. Without it the supervisor
// cannot open its log file and the service dies during startup, which the service control
// manager reports as an opaque start failure.
//
// This runs on every start rather than only when the service is registered: an installation
// whose service is otherwise correct may still be missing the grant, and the log directory
// holds few enough files for the walk to be cheap.
func (pm *Shawl) grantLogDirAccess(ctx context.Context, account string) error {
	logDir := pm.logDir()

	if err := oscore.Grant(ctx, logDir, account, oscore.GrantFlagModify); err != nil {
		return errors.WithMessagef(err, "failed to grant permissions to %s for %s", account, logDir)
	}

	return nil
}

func (pm *Shawl) removeService(ctx context.Context, serviceName string, out io.Writer) {
	if err := stopService(serviceName); err != nil &&
		!errors.Is(err, ErrServiceNotFound) &&
		!isServiceErrno(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		logger.WithError(ctx, err).Warn("failed to stop service before registering it again")
	}

	if err := pm.waitForServiceState(ctx, serviceName, svc.Stopped, stopTimeout); err != nil {
		logger.WithError(ctx, err).Warn("failed to wait for service to stop before registering it again")
	}

	if err := deleteService(serviceName); err != nil && !errors.Is(err, ErrServiceNotFound) {
		logger.WithError(ctx, err).Warn("failed to delete service before registering it again")
	}

	// DeleteService only marks the service for deletion while a handle to it is still open, and
	// creating it again before it is gone fails with ERROR_SERVICE_MARKED_FOR_DELETE.
	if err := pm.waitForServiceRemoved(ctx, serviceName); err != nil {
		logger.WithError(ctx, err).Warn("failed to wait for service removal")
	}

	_, _ = out.Write([]byte("Removed service " + serviceName + "\n"))
}

func (pm *Shawl) waitForServiceState(
	ctx context.Context, serviceName string, want svc.State, timeout time.Duration,
) error {
	ticker := time.NewTicker(stateTickerInterval)
	defer ticker.Stop()

	deadline := time.After(timeout)

	var last svc.State

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.WithMessagef(
				ErrServiceStateTimeout,
				"service %q is %s, expected %s",
				serviceName, serviceStateName(uint32(last)), serviceStateName(uint32(want)),
			)
		case <-ticker.C:
			status, err := queryService(serviceName)
			if errors.Is(err, ErrServiceNotFound) {
				if want == svc.Stopped {
					return nil
				}

				return err
			}
			if err != nil {
				return err
			}

			last = status.State

			if status.State == want {
				return nil
			}

			if want == svc.Running && status.State == svc.Stopped {
				return errors.WithMessagef(
					ErrServiceStoppedOnStart,
					"service %q, Win32 exit code %d, service exit code %d",
					serviceName, status.Win32ExitCode, status.ServiceSpecificExitCode,
				)
			}
		}
	}
}

func (pm *Shawl) waitForServiceRemoved(ctx context.Context, serviceName string) error {
	ticker := time.NewTicker(stateTickerInterval)
	defer ticker.Stop()

	deadline := time.After(deleteTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return errors.WithMessagef(ErrServiceStateTimeout, "service %q was not removed", serviceName)
		case <-ticker.C:
			installed, err := serviceInstalled(serviceName)
			if err != nil {
				return err
			}

			if !installed {
				return nil
			}
		}
	}
}

// waitForServiceRunning turns "the service control manager accepted the start request" into
// "the service is running". A service that dies on startup would otherwise be reported as
// started. Note that shawl restarts the game process itself, so a running service proves the
// supervisor came up, not that the game stayed up; the log tail covers the rest.
func (pm *Shawl) waitForServiceRunning(ctx context.Context, server *domain.Server, out io.Writer) error {
	serviceName := pm.serviceName(server)

	err := pm.waitForServiceState(ctx, serviceName, svc.Running, startTimeout)
	if err != nil {
		if errors.Is(err, ErrServiceStoppedOnStart) {
			_, _ = out.Write([]byte("Service " + serviceName + " stopped immediately after start\n"))
			pm.writeLogTail(out, server)
		}

		return err
	}

	_, _ = out.Write([]byte("Service " + serviceName + " is running\n"))

	return nil
}

// seekLogTail positions f at the last shawlOutputSizeLimit bytes of the log. It reports whether
// the file was long enough to be cut, in which case the read starts inside an entry whose
// beginning is gone and the caller has to discard the fragment before the first newline.
func seekLogTail(f *os.File) (bool, error) {
	stat, err := f.Stat()
	if err != nil {
		return false, errors.Wrap(err, "failed to get file stat")
	}

	if stat.Size() <= shawlOutputSizeLimit {
		return false, nil
	}

	if _, err := f.Seek(-shawlOutputSizeLimit, io.SeekEnd); err != nil {
		return false, errors.Wrap(err, "failed to seek file")
	}

	return true, nil
}

func (pm *Shawl) writeLogTail(out io.Writer, server *domain.Server) {
	f, err := os.Open(pm.logPath(server))
	if err != nil {
		return
	}
	defer func() {
		_ = f.Close()
	}()

	// Only the end of the log can hold the lines that explain the failure, and the file grows
	// until the daily rotation, so it is read from the same bound GetOutput uses.
	truncated, err := seekLogTail(f)
	if err != nil {
		return
	}

	lines, err := readShawlLogTail(f, shawlLogTailLines, truncated)
	if err != nil || len(lines) == 0 {
		return
	}

	_, _ = out.Write([]byte("Last lines of the service log:\n"))

	for _, line := range lines {
		_, _ = out.Write([]byte(line + "\n"))
	}
}

// writeServiceError reports a failed service operation. The numeric code and its symbol are
// locale-invariant; the sentence the OS formats for the code is not, so it is printed as
// supporting detail rather than as the diagnosis.
func writeServiceError(out io.Writer, err error, account, logDir string) {
	code, ok := serviceErrorCode(err)
	if !ok {
		_, _ = out.Write([]byte("[SCM] " + err.Error() + "\n"))

		return
	}

	line := "[SCM] " + strconv.FormatUint(uint64(code), 10)
	if symbol := serviceErrorSymbol(code); symbol != "" {
		line += " " + symbol
	}

	_, _ = out.Write([]byte(line + "\n"))
	_, _ = out.Write([]byte("[SCM] " + err.Error() + "\n"))

	if hint := serviceErrorHint(code, account, logDir); hint != "" {
		_, _ = out.Write([]byte("Hint: " + hint + "\n"))
	}
}

func (pm *Shawl) accountHintFor(server *domain.Server) string {
	if pm.cfg.UseNetworkServiceUser {
		return oscore.WindowsNetworkServiceAccount
	}

	return server.User()
}

func (pm *Shawl) ensureDirs(out io.Writer) error {
	for _, dir := range []string{shawlServicesConfigPath, pm.logDir()} {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			_, _ = out.Write([]byte("Creating directory " + dir + "\n"))

			if err := os.MkdirAll(dir, 0755); err != nil {
				return errors.Wrapf(err, "failed to create directory %s", dir)
			}
		}
	}

	return nil
}

func (pm *Shawl) serviceName(server *domain.Server) string {
	return shawlServicePrefix + strconv.Itoa(server.ID())
}

func (pm *Shawl) configFile(server *domain.Server) string {
	return filepath.Join(shawlServicesConfigPath, pm.serviceName(server)+".yaml")
}

func (pm *Shawl) logDir() string {
	return filepath.Join(shawlServicesConfigPath, "logs")
}

func (pm *Shawl) logPath(server *domain.Server) string {
	return filepath.Join(pm.logDir(), pm.serviceName(server)+".log_rCURRENT.log")
}

func (pm *Shawl) Attach(
	_ context.Context, _ *domain.Server, _ io.Reader, _ io.Writer,
) error {
	return ErrNotImplemented
}

func (pm *Shawl) HasOwnInstallation(_ *domain.Server) bool {
	return false
}

// Metrics returns only the cached process-active gauge for now. Resource
// stats via Windows performance counters / WMI are tracked as a follow-up.
func (pm *Shawl) Metrics(_ context.Context, server *domain.Server) ([]domain.Metric, error) {
	return []domain.Metric{livenessMetric(server, time.Now())}, nil
}
