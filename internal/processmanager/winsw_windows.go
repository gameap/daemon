//go:build windows

package processmanager

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/pkg/errors"
)

const (
	servicesConfigPath = "C:\\gameap\\services"
	servicePrefix      = "gameapServer"

	outputSizeLimit = 30000

	errorCodeCannotStart     = 1053
	errorCodeServiceNotExist = 1060

	commandInstall   = "install"
	commandRefresh   = "refresh"
	commandUninstall = "uninstall"
	commandStart     = "start"
	commandStop      = "stop"
	commandRestart   = "restart"
	commandStatus    = "status"
)

type WinSW struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewWinSW(cfg *config.Config, _, detailedExecutor contracts.Executor) *WinSW {
	return &WinSW{
		cfg:      cfg,
		executor: detailedExecutor,
	}
}

func (pm *WinSW) Install(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	createdNewService, err := pm.makeService(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	var result domain.Result

	if createdNewService {
		result, err = pm.runWinSWCommand(ctx, commandInstall, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to install service")
		}
		if result != domain.SuccessResult {
			return domain.ErrorResult, errors.New("failed to install service")
		}
	} else {
		result, err = pm.runWinSWCommand(ctx, commandRefresh, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to refresh service")
		}
	}

	return domain.SuccessResult, nil
}

func (pm *WinSW) Uninstall(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.runWinSWCommand(ctx, commandUninstall, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to run uninstall command")
	}

	err = os.Remove(pm.serviceFile(server))
	if err != nil {
		logger.WithError(ctx, err).Warn("failed to remove service file")
	}

	return domain.SuccessResult, nil
}

func (pm *WinSW) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	return pm.command(ctx, server, commandStart, out)
}

func (pm *WinSW) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	_, err := pm.runWinSWCommand(ctx, commandStop, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to run stop command")
	}

	return domain.SuccessResult, nil
}

func (pm *WinSW) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	return pm.command(ctx, server, commandRestart, out)
}

const (
	exitCodeStatusNotActive = 0
	exitCodeStatusActive    = 1
)

func (pm *WinSW) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	if _, err := os.Stat(pm.serviceFile(server)); err != nil {
		logger.Debug(ctx, "Service file not found")
		return domain.ErrorResult, nil
	}

	result, err := pm.runWinSWCommand(ctx, commandStatus, server, out)
	if err != nil {
		return domain.ErrorResult, errors.Wrap(err, "failed to get daemon status")
	}

	if result == exitCodeStatusNotActive {
		return domain.ErrorResult, nil
	}

	if result == exitCodeStatusActive {
		return domain.SuccessResult, nil
	}

	// If we are here, it means that we have unexpected result
	return domain.ErrorResult, nil
}

func (pm *WinSW) runWinSWCommand(
	ctx context.Context, command string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	result, err := pm.executor.ExecWithWriter(
		ctx,
		fmt.Sprintf("winsw %s %s ", command, pm.serviceFile(server)),
		out,
		contracts.ExecutorOptions{
			WorkDir: pm.cfg.WorkDir(),
		},
	)
	return domain.Result(result), err
}

func (pm *WinSW) command(
	ctx context.Context, server *domain.Server, command string, out io.Writer,
) (domain.Result, error) {
	err := checkUser(server.User())
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to check user")
	}

	createdNewService, err := pm.makeService(ctx, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to make service")
	}

	var result domain.Result

	if createdNewService {
		result, err = pm.runWinSWCommand(ctx, commandInstall, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to install service")
		}
		if result != domain.SuccessResult {
			return domain.ErrorResult, errors.New("failed to install service")
		}
	} else {
		result, err = pm.runWinSWCommand(ctx, commandRefresh, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to refresh service")
		}
		if result == errorCodeServiceNotExist {
			logger.Warn(ctx, "failed to refresh service config, trying to install service")

			result, err = pm.runWinSWCommand(ctx, commandInstall, server, out)
			if err != nil {
				return domain.ErrorResult, errors.WithMessage(err, "failed to install service")
			}
			if result != domain.SuccessResult {
				return domain.ErrorResult, errors.New("failed to refresh and install service")
			}
		} else if result != domain.SuccessResult {
			return domain.ErrorResult, errors.New("failed to refresh service")
		}
	}

	result, err = pm.runWinSWCommand(ctx, command, server, out)
	if err != nil {
		return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
	}

	if result == errorCodeCannotStart && command == commandStart {
		_, err = pm.tryFixReinstallService(ctx, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to try fix by reinstalling service")
		}

		result, err = pm.runWinSWCommand(ctx, command, server, out)
		if err != nil {
			return domain.ErrorResult, errors.WithMessage(err, "failed to exec command")
		}
	}

	return result, nil
}

func (pm *WinSW) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	f, err := os.Open(pm.logPath(server))
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

func (pm *WinSW) SendInput(
	ctx context.Context, input string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	return domain.ErrorResult, errors.New("input is not supported on Windows")
}

func (pm *WinSW) tryFixReinstallService(
	ctx context.Context, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	result, err := pm.runWinSWCommand(ctx, commandUninstall, server, out)
	if err != nil {
		logger.Warn(ctx, errors.WithMessage(err, "failed to uninstall service"))
	}

	result, err = pm.runWinSWCommand(ctx, commandInstall, server, out)
	if err != nil {
		logger.Warn(ctx, errors.WithMessage(err, "failed to install service"))
	}

	if result != domain.SuccessResult {
		return domain.ErrorResult, errors.New("failed to install service")
	}

	return domain.SuccessResult, nil
}

func checkUser(name string) error {
	if name == "" {
		return ErrEmptyUser
	}

	_, err := user.Lookup(name)
	if err != nil {
		return errors.WithMessage(err, "failed to lookup user")
	}

	return nil
}

func (pm *WinSW) makeService(ctx context.Context, server *domain.Server, out io.Writer) (bool, error) {
	serviceFile := pm.serviceFile(server)

	if _, err := os.Stat(servicesConfigPath); errors.Is(err, os.ErrNotExist) {
		_, _ = out.Write([]byte("Creating directory " + servicesConfigPath + "\n"))
		err := os.MkdirAll(servicesConfigPath, 0755)
		if err != nil {
			return false, errors.WithMessage(err, "failed to create directory")
		}
	}

	serviceFileNotExist := false

	_, err := os.Stat(serviceFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.WithMessage(err, "failed to check file")
	}
	if err != nil && errors.Is(err, os.ErrNotExist) {
		_, _ = out.Write([]byte("Service file not found\n"))
		serviceFileNotExist = true
	}

	serviceConfig, err := pm.buildServiceConfig(server)
	if err != nil {
		return false, errors.WithMessage(err, "failed to build service config")
	}

	// If service file exists, check if it's the same
	if !serviceFileNotExist {
		_, _ = out.Write([]byte("Service file exists\n"))
		_, _ = out.Write([]byte("Checking if service configuration wasn't changed\n"))

		oldServiceConfig, err := os.ReadFile(serviceFile)
		if err != nil {
			return false, errors.WithMessage(err, "failed to read file")
		}

		if string(oldServiceConfig) == serviceConfig {
			_, _ = out.Write([]byte("Service configuration wasn't changed\n"))
			return false, nil
		}

		_, _ = out.Write([]byte("Service configuration was changed\n"))
	}

	flag := os.O_TRUNC | os.O_WRONLY
	if serviceFileNotExist {
		flag = os.O_CREATE | os.O_WRONLY
	}

	f, err := os.OpenFile(serviceFile, flag, 0644)
	if err != nil {
		return false, errors.WithMessage(err, "failed to open file")
	}
	defer func() {
		err := f.Close()
		if err != nil {
			logger.Warn(ctx, errors.WithMessage(err, "failed to close file"))
		}
	}()

	_, err = f.WriteString(serviceConfig)
	if err != nil {
		return false, errors.WithMessage(err, "failed to write to file")
	}

	err = f.Sync()
	if err != nil {
		return false, errors.WithMessage(err, "failed to sync file")
	}

	return serviceFileNotExist, nil
}

func (pm *WinSW) buildServiceConfig(server *domain.Server) (string, error) {
	cmd := domain.MakeFullCommand(
		pm.cfg,
		server,
		pm.cfg.Scripts.Start,
		server.StartCommand(),
	)

	if cmd == "" {
		return "", ErrEmptyCommand
	}

	cmdArr, err := shellquote.Split(cmd)
	if err != nil {
		return "", errors.WithMessage(err, "failed to split command")
	}

	executable := cmdArr[0]

	argArr := make([]string, 0, len(cmdArr)*2)

	if filepath.Ext(executable) == ".bat" {
		executable = "cmd.exe"
		argArr = append(argArr, "/c", cmdArr[0])
	}

	argArr = append(argArr, cmdArr[1:]...)

	var arguments string

	if len(cmdArr) > 1 {
		arguments = strings.Join(argArr, " ")
	}

	serviceName := pm.serviceName(server)
	serviceConfig := WinSWServiceConfig{
		ID:               serviceName,
		Name:             serviceName,
		Executable:       executable,
		Arguments:        arguments,
		WorkingDirectory: server.WorkDir(pm.cfg),
		Log: log{
			Mode: "reset",
		},
		OnFailure: []onFailure{
			{Action: "restart", Delay: "1 sec"},
			{Action: "restart", Delay: "2 sec"},
			{Action: "restart", Delay: "5 sec"},
			{Action: "restart", Delay: "5 sec"},
		},
		ResetFailure: "1 hour",
		AutoRefresh:  "false",
	}

	rawPw, exists := pm.cfg.Users[server.User()]
	if !exists {
		return "", ErrUserNotFound
	}

	if rawPw == "" {
		return "", ErrInvalidUserPassword
	}

	var password string

	switch {
	case strings.HasPrefix(rawPw, "base64:"):
		pw, err := base64.StdEncoding.DecodeString(rawPw[7:])
		if err != nil {
			return "", errors.WithMessage(err, "failed to decode base64 password")
		}
		password = string(pw)
	default:
		password = rawPw
	}

	serviceConfig.ServiceAccount.Username = server.User()
	serviceConfig.ServiceAccount.Password = password

	out, err := xml.MarshalIndent(struct {
		WinSWServiceConfig
		XMLName struct{} `xml:"service"`
	}{WinSWServiceConfig: serviceConfig}, "", "  ")
	if err != nil {
		return "", errors.WithMessage(err, "failed to marshal xml")
	}

	return string(out), nil
}

func (pm *WinSW) serviceName(server *domain.Server) string {
	builder := strings.Builder{}
	builder.Grow(50)

	builder.WriteString(servicePrefix)
	builder.WriteString(strconv.Itoa(server.ID()))

	return builder.String()
}

func (pm *WinSW) serviceFile(server *domain.Server) string {
	return filepath.Join(servicesConfigPath, pm.serviceName(server)+".xml")
}

func (pm *WinSW) logPath(server *domain.Server) string {
	return filepath.Join(servicesConfigPath, pm.serviceName(server)+".out.log")
}

type WinSWServiceConfig struct {
	ID               string `xml:"id"`
	Name             string `xml:"name"`
	Executable       string `xml:"executable"`
	Arguments        string `xml:"arguments,omitempty"`
	WorkingDirectory string `xml:"workingdirectory,omitempty"`

	StopExecutable string `xml:"stopexecutable,omitempty"`
	StopArguments  string `xml:"stoparguments,omitempty"`
	StopTimeout    string `xml:"stoptimeout,omitempty"`

	OnFailure    []onFailure `xml:"onfailure,omitempty"`
	ResetFailure string      `xml:"resetfailure,omitempty"`

	AutoRefresh string `xml:"autoRefresh,omitempty"`

	Logpath string `xml:"logpath,omitempty"`
	Log     log    `xml:"log,omitempty"`

	ServiceAccount struct {
		Username string `xml:"username,omitempty"`
		Password string `xml:"password,omitempty"`
	} `xml:"serviceaccount,omitempty"`
}

type onFailure struct {
	Action string `xml:"action,attr"`
	Delay  string `xml:"delay,attr,omitempty"`
}

type log struct {
	Mode string `xml:"mode,attr"`
}
