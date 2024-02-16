package definitions

import (
	"context"
	"net/http"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/contracts"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/gameap/daemon/internal/processmanager"
	"github.com/go-resty/resty/v2"
)

func CreateServicesResty(ctx context.Context, c Container) *resty.Client {
	restyClient := resty.New()
	restyClient.SetBaseURL(c.Cfg(ctx).APIHost)
	restyClient.SetHeader("User-Agent", "GameAP Daemon/3.0")
	restyClient.RetryCount = 30
	restyClient.RetryMaxWaitTime = 10 * time.Minute
	restyClient.SetTimeout(10 * time.Second)
	restyClient.SetLogger(c.Logger(ctx))

	restyClient.AddRetryCondition(
		func(r *resty.Response, err error) bool {
			return r.StatusCode() == http.StatusTooManyRequests ||
				r.StatusCode() == http.StatusBadGateway
		},
	)

	return restyClient
}

func CreateServicesAPICaller(ctx context.Context, c Container) contracts.APIRequestMaker {
	client, err := services.NewAPICaller(
		ctx,
		c.Cfg(ctx),
		c.Services().Resty(ctx),
	)

	if err != nil {
		c.SetError(err)
		return nil
	}

	return client
}

func CreateServicesExecutor(_ context.Context, _ Container) contracts.Executor {
	return components.NewExecutor()
}

func CreateServiceExtendableExecutor(ctx context.Context, c Container) contracts.Executor {
	executor := components.NewDefaultExtendableExecutor(c.Services().Executor(ctx))

	executor.RegisterHandler("get-tool", customhandlers.NewGetTool(c.Cfg(ctx)).Handle)

	return executor
}

func CreateServicesProcessManager(ctx context.Context, c Container) contracts.ProcessManager {
	return processmanager.NewTmux(
		c.Cfg(ctx),
		c.Services().Executor(ctx),
	)
}

func CreateServicesGdTaskManager(ctx context.Context, c Container) *gdaemonscheduler.TaskManager {
	return gdaemonscheduler.NewTaskManager(
		c.Repositories().GdTaskRepository(ctx),
		c.CacheManager(ctx),
		c.ServerCommandFactory(ctx),
		c.Services().ExtendableExecutor(ctx),
		c.Cfg(ctx),
	)
}
