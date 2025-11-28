//go:build linux

package processmanager

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
)

func Load(
	name string, cfg *config.Config, executor, detailedExecutor contracts.Executor,
) (contracts.ProcessManager, error) {
	switch name {
	case "tmux":
		return NewTmux(cfg, executor, detailedExecutor), nil
	case "systemd":
		return NewSystemD(cfg, executor, detailedExecutor), nil
	case "simple":
		return NewSimple(cfg, executor, detailedExecutor), nil
	default:
		return nil, ErrUnknownProcessManager
	}
}
