package definitions

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/contracts"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/processmanager"
)

func CreateServicesExecutor(_ context.Context, _ Container) contracts.Executor {
	return components.NewCleanExecutor()
}

func CreateServiceExtendableExecutor(ctx context.Context, c Container) contracts.Executor {
	executor := components.NewDefaultExtendableExecutor(components.NewExecutor())

	executor.RegisterHandler("get-tool", customhandlers.NewGetTool(c.Cfg(ctx)).Handle)
	executor.RegisterHandler(
		"server-output",
		customhandlers.NewOutputReader(
			c.Services().ProcessManager(ctx),
			c.Repositories().ServerRepository(ctx),
		).Handle,
	)

	executor.RegisterHandler(
		"server-command",
		customhandlers.NewCommandSender(
			c.Services().ProcessManager(ctx),
			c.Repositories().ServerRepository(ctx),
		).Handle,
	)

	return executor
}

func CreateServicesProcessManager(ctx context.Context, c Container) contracts.ProcessManager {
	pm, err := processmanager.Load(
		c.Cfg(ctx).ProcessManager.Name,
		c.Cfg(ctx),
		c.Services().Executor(ctx),
		components.NewExecutor(),
	)
	if err != nil {
		c.SetError(err)
		return nil
	}

	return pm
}

func CreateServicesGdTaskManager(ctx context.Context, c Container) *gdaemonscheduler.TaskManager {
	return gdaemonscheduler.NewTaskManager(
		c.CacheManager(ctx),
		c.ServerCommandFactory(ctx),
		c.Services().ExtendableExecutor(ctx),
		c.Cfg(ctx),
	)
}
