//go:build linux
// +build linux

package processmanager

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
)

const (
	Default = "tmux"
)

func Load(name string, cfg *config.Config, executor contracts.Executor) (contracts.ProcessManager, error) {
	switch name {
	case "tmux":
		return NewTmux(cfg, executor), nil
	case "systemd":
		return NewSystemD(cfg, executor), nil
	case "simple":
		return NewSimple(cfg, executor), nil
	default:
		return nil, ErrUnknownProcessManager
	}
}
