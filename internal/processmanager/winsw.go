//go:build windows
// +build windows

package processmanager

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

const (
	servicesConfigPath = "C:\\gameap\\services"
)

type WindowsService struct {
	cfg      *config.Config
	executor contracts.Executor
}

func NewWindowsService(cfg *config.Config, executor contracts.Executor) *WindowsService {
	return &WindowsService{
		cfg:      cfg,
		executor: executor,
	}
}

func (ws *WindowsService) Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

func (ws *WindowsService) Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

func (ws *WindowsService) Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

func (ws *WindowsService) Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

func (ws *WindowsService) GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

func (ws *WindowsService) SendInput(
	ctx context.Context, input string, server *domain.Server, out io.Writer,
) (domain.Result, error) {
	//TODO implement me
	panic("implement me")
}

type WinSWServiceConfig struct {
	ID               string `xml:"id"`
	Name             string `xml:"name"`
	Executable       string `xml:"executable"`
	WorkingDirectory string `xml:"workingdirectory,omitempty"`
	Arguments        string `xml:"arguments,omitempty"`

	StopExecutable string `xml:"stopexecutable,omitempty"`
	StopArguments  string `xml:"stoparguments,omitempty"`

	OnFailure    []onFailure `xml:"onfailure,omitempty"`
	ResetFailure string      `xml:"resetfailure,omitempty"`

	ServiceAccount struct {
		Username string `xml:"username,omitempty"`
		Password string `xml:"password,omitempty"`
	} `xml:"serviceaccount,omitempty"`
}

type onFailure struct {
	Action string `xml:"action,attr"`
	Delay  string `xml:"delay,attr,omitempty"`
}
