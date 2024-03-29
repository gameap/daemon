package internal

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"

	"github.com/gameap/daemon/internal/app/di/internal/definitions"
	"github.com/gameap/daemon/internal/app/domain"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
)

type Container struct {
	err error

	cfg                  *config.Config
	logger               *logrus.Logger
	processRunner        *services.Runner
	cacheManager         contracts.Cache
	serverCommandFactory *gameservercommands.ServerCommandFactory

	services     *ServicesContainer
	repositories *RepositoryContainer
}

func NewContainer() *Container {
	c := &Container{}
	c.services = &ServicesContainer{Container: c}
	c.repositories = &RepositoryContainer{Container: c}

	return c
}

// Error returns the first initialization error, which can be set via SetError in a service definition.
func (c *Container) Error() error {
	return c.err
}

// SetError sets the first error into container.
// The error is used in the public container to return an initialization error.
func (c *Container) SetError(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

type ServicesContainer struct {
	*Container

	resty          *resty.Client
	apiCaller      contracts.APIRequestMaker
	executor       contracts.Executor
	processManager contracts.ProcessManager
	gdTaskManager  *gdaemonscheduler.TaskManager
}

type RepositoryContainer struct {
	*Container

	gdTaskRepository     domain.GDTaskRepository
	serverRepository     domain.ServerRepository
	serverTaskRepository domain.ServerTaskRepository
}

func (c *Container) Cfg(_ context.Context) *config.Config {
	return c.cfg
}

func (c *Container) Logger(_ context.Context) *logrus.Logger {
	return c.logger
}

func (c *Container) ProcessRunner(ctx context.Context) *services.Runner {
	if c.processRunner == nil && c.err == nil {
		c.processRunner = definitions.CreateProcessRunner(ctx, c)
	}
	return c.processRunner
}

func (c *Container) CacheManager(ctx context.Context) contracts.Cache {
	if c.cacheManager == nil && c.err == nil {
		c.cacheManager = definitions.CreateCacheManager(ctx, c)
	}
	return c.cacheManager
}

func (c *Container) ServerCommandFactory(ctx context.Context) *gameservercommands.ServerCommandFactory {
	if c.serverCommandFactory == nil && c.err == nil {
		c.serverCommandFactory = definitions.CreateServerCommandFactory(ctx, c)
	}
	return c.serverCommandFactory
}

func (c *Container) Services() definitions.ServicesContainer {
	return c.services
}

func (c *ServicesContainer) Resty(ctx context.Context) *resty.Client {
	if c.resty == nil && c.err == nil {
		c.resty = definitions.CreateServicesResty(ctx, c)
	}
	return c.resty
}

func (c *ServicesContainer) APICaller(ctx context.Context) contracts.APIRequestMaker {
	if c.apiCaller == nil && c.err == nil {
		c.apiCaller = definitions.CreateServicesAPICaller(ctx, c)
	}
	return c.apiCaller
}

func (c *ServicesContainer) Executor(ctx context.Context) contracts.Executor {
	if c.executor == nil && c.err == nil {
		c.executor = definitions.CreateServicesExecutor(ctx, c)
	}
	return c.executor
}

func (c *ServicesContainer) ExtendableExecutor(ctx context.Context) contracts.Executor {
	if c.executor == nil && c.err == nil {
		c.executor = definitions.CreateServiceExtendableExecutor(ctx, c)
	}
	return c.executor
}

func (c *ServicesContainer) ProcessManager(ctx context.Context) contracts.ProcessManager {
	if c.processManager == nil && c.err == nil {
		c.processManager = definitions.CreateServicesProcessManager(ctx, c)
	}

	return c.processManager
}

func (c *ServicesContainer) GdTaskManager(ctx context.Context) *gdaemonscheduler.TaskManager {
	if c.gdTaskManager == nil && c.err == nil {
		c.gdTaskManager = definitions.CreateServicesGdTaskManager(ctx, c)
	}
	return c.gdTaskManager
}

func (c *Container) Repositories() definitions.RepositoryContainer {
	return c.repositories
}

func (c *RepositoryContainer) GdTaskRepository(ctx context.Context) domain.GDTaskRepository {
	if c.gdTaskRepository == nil && c.err == nil {
		c.gdTaskRepository = definitions.CreateRepositoriesGdTaskRepository(ctx, c)
	}
	return c.gdTaskRepository
}

func (c *RepositoryContainer) ServerRepository(ctx context.Context) domain.ServerRepository {
	if c.serverRepository == nil && c.err == nil {
		c.serverRepository = definitions.CreateRepositoriesServerRepository(ctx, c)
	}
	return c.serverRepository
}

func (c *RepositoryContainer) ServerTaskRepository(ctx context.Context) domain.ServerTaskRepository {
	if c.serverTaskRepository == nil && c.err == nil {
		c.serverTaskRepository = definitions.CreateRepositoriesServerTaskRepository(ctx, c)
	}
	return c.serverTaskRepository
}

func (c *Container) SetCfg(s *config.Config) {
	c.cfg = s
}

func (c *Container) SetLogger(s *logrus.Logger) {
	c.logger = s
}

func (c *ServicesContainer) SetAPICaller(s contracts.APIRequestMaker) {
	c.apiCaller = s
}

func (c *Container) Close() {}
