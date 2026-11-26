//go:build darwin

package processmanager

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/fsutil"
)

// Load creates the process manager the config names. confineOpts name the
// directories a manager that writes into a server directory is confined by
// besides that directory, see fsutil.OpenDir.
func Load(
	name string, cfg *config.Config,
	executor contracts.Executor,
	detailedExecutor contracts.Executor,
	confineOpts ...fsutil.ConfineOption,
) (contracts.ProcessManager, error) {
	switch name {
	case "tmux":
		return NewTmux(cfg, executor, detailedExecutor), nil
	case "simple":
		return NewSimple(cfg, executor, detailedExecutor), nil
	case "docker":
		return NewDocker(cfg, executor, detailedExecutor, confineOpts...), nil
	case "podman":
		return NewPodman(cfg, executor, detailedExecutor, confineOpts...), nil
	default:
		return nil, ErrUnknownProcessManager
	}
}
