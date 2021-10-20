package app

import (
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/go-resty/resty/v2"
	"github.com/sarulabs/di"
)

func newBuilder(cfg *config.Config) (*di.Builder, error) {
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

	gdaemonTasksRepositoryDef = "gdaemonTasksRepository"

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
			Name: restyDef,
			Build: func(ctn di.Container) (interface{}, error) {
				restyClient := resty.New()
				restyClient.SetHostURL(cfg.APIHost)
				restyClient.SetHeader("User-Agent", "GameAP Daemon/3.0")

				return restyClient, nil
			},
		},
		// Repositories
		{
			Name: gdaemonTasksRepositoryDef,
			Build: func(ctn di.Container) (interface{}, error) {
				restyClient := ctn.Get(restyDef).(*resty.Client)

				return repositories.NewGDTasksRepository(restyClient), nil
			},
		},
		// Factories
		{
			Name: serverCommandFactoryDef,
			Build: func(ctn di.Container) (interface{}, error) {
				return game_server_commands.NewFactory(cfg), nil
			},
		},
	}
}
