package definitions

import (
	"context"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/fsutil"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gameap/daemon/internal/app/services"
)

func CreateProcessRunner(ctx context.Context, c Container) *services.Runner {
	processRunner, err := services.NewProcessRunner(
		c.Cfg(ctx),
		c.ServerCommandFactory(ctx),
		c.Services().GdTaskManager(ctx),
		c.Repositories().ServerRepository(ctx),
	)
	if err != nil {
		c.SetError(err)
		return nil
	}

	return processRunner
}

func CreateCacheManager(ctx context.Context, c Container) contracts.Cache {
	cache, err := services.NewLocalCache(c.Cfg(ctx))
	if err != nil {
		c.SetError(err)
		return nil
	}

	return cache
}

func CreateServerCommandFactory(ctx context.Context, c Container) *gameservercommands.ServerCommandFactory {
	return gameservercommands.NewFactory(
		c.Cfg(ctx),
		c.Repositories().ServerRepository(ctx),
		c.Services().Executor(ctx),
		c.Services().ProcessManager(ctx),
		serverDirBoundaries(ctx, c)...,
	)
}

// serverDirBoundaries makes the directory of every game server the daemon
// knows a boundary for whatever is opened inside the work directory.
func serverDirBoundaries(ctx context.Context, c Container) []fsutil.ConfineOption {
	serverRepo := c.Repositories().ServerRepository(ctx).(*repositories.ServerRepository)

	return []fsutil.ConfineOption{fsutil.WithServerDirs(serverRepo)}
}
