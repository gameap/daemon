//go:build windows
// +build windows

package processmanager

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
)

func Load(name string, cfg *config.Config, executor contracts.Executor) (contracts.ProcessManager, error) {
	switch name {
	case "winsw":
		return NewWindowsService(cfg, executor), nil
	case "simple":
		return NewSimple(cfg, executor), nil
	default:
		return nil, ErrUnknownProcessManager
	}
}
