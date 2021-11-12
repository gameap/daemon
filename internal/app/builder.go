package app

import (
	"context"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/go-resty/resty/v2"
	"github.com/sarulabs/di"
)

func NewBuilder(cfg *config.Config) (*di.Builder, error) {
	builder, err := di.NewBuilder()
	if err != nil {
		return nil, err
	}

	err = builder.Add(definitions(cfg)...)
	if err != nil {
		return nil, err
	}

	return builder, nil
}

const (
	restyDef        = "resty"
	cacheManagerDef = "cacheManager"
	storeDef        = "store"
	apiCallerDef    = "apiCaller"
	executorDef     = "executorDef"

	gdaemonTasksRepositoryDef = "gdaemonTasksRepository"
	serverRepositoryDef       = "serverRepository"

	serverCommandFactoryDef = "serverCommandFactory"
)

func definitions(cfg *config.Config) []di.Def {
	return []di.Def{
		{
			Name: cacheManagerDef,
			Build: func(ctn di.Container) (interface{}, error) {
				return NewLocalCache(cfg)
			},
		},
		{
			Name: storeDef,
			Build: func(ctn di.Container) (interface{}, error) {
				return NewLocalStore(cfg)
			},
		},
		{
			Name: apiCallerDef,
			Build: func(ctn di.Container) (interface{}, error) {
				return NewAPICaller(
					context.TODO(),
					cfg,
					ctn.Get(restyDef).(*resty.Client),
				)
			},
		},
		{
			Name: restyDef,
			Build: func(ctn di.Container) (interface{}, error) {
				restyClient := resty.New()
				restyClient.SetHostURL(cfg.APIHost)
				restyClient.SetHeader("User-Agent", "GameAP Daemon/3.0")

				return restyClient, nil
			},
		},
		{
			Name: executorDef,
			Build: func(ctn di.Container) (interface{}, error) {
				return components.NewExecutor(), nil
			},
		},
		// Repositories
		{
			Name: gdaemonTasksRepositoryDef,
			Build: func(ctn di.Container) (interface{}, error) {
				apiClient := ctn.Get(apiCallerDef).(interfaces.APIRequestMaker)
				serverRepository := ctn.Get(serverRepositoryDef).(domain.ServerRepository)

				return repositories.NewGDTasksRepository(
					apiClient,
					serverRepository,
				), nil
			},
		},
		{
			Name: serverRepositoryDef,
			Build: func(ctn di.Container) (interface{}, error) {
				apiClient := ctn.Get(apiCallerDef).(interfaces.APIRequestMaker)

				return repositories.NewServerRepository(apiClient), nil
			},
		},
		// Factories
		{
			Name: serverCommandFactoryDef,
			Build: func(ctn di.Container) (interface{}, error) {
				serverRepository := ctn.Get(serverRepositoryDef).(domain.ServerRepository)
				executor := ctn.Get(executorDef).(interfaces.Executor)

				return game_server_commands.NewFactory(
					cfg,
					serverRepository,
					executor,
				), nil
			},
		},
	}
}
