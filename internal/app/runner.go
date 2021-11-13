package app

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/servers_loop"
	"github.com/sarulabs/di"
)

type runner struct {
	container di.Container
}

func newProcessManager(cfg *config.Config) (*runner, error) {
	builder, err := NewBuilder(cfg)
	if err != nil {
		return nil, err
	}

	container := builder.Build()

	return &runner{container}, nil
}

func (r *runner) init(ctx context.Context, cfg *config.Config) error {
	err := r.initNodeConfigFromAPI(ctx, cfg)
	if err != nil {
		return err
	}

	return nil
}

func (r *runner) initNodeConfigFromAPI(ctx context.Context, cfg *config.Config) error {
	cfgInitializer := config.NewNodeConfigInitializer(r.container.Get(apiCallerDef).(interfaces.APIRequestMaker))

	return cfgInitializer.Initialize(ctx, cfg)
}

func (r *runner) runGDaemonServer(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		srv, err := server.NewServer(
			cfg.ListenIP,
			cfg.ListenPort,
			cfg.CertificateChainFile,
			cfg.PrivateKeyFile,
			server.CredentialsConfig{
				PasswordAuthentication: cfg.PasswordAuthentication,
				Login:                  cfg.DaemonLogin,
				Password:               cfg.DaemonPassword,
			},
			r.container.Get(gdTaskManger).(domain.GDTaskStatsReader),
		)
		if err != nil {
			return err
		}

		return srv.Run(ctx)
	}
}

func (r *runner) runGDaemonTaskScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		taskManager := r.container.Get(gdTaskManger).(*gdaemon_scheduler.TaskManager)

		return taskManager.Run(ctx)
	}
}

func (r *runner) runServersLoop(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		loop := servers_loop.NewServersLoop(
			r.container.Get(serverRepositoryDef).(domain.ServerRepository),
			r.container.Get(serverCommandFactoryDef).(*game_server_commands.ServerCommandFactory),
			cfg,
		)

		return loop.Run(ctx)
	}
}
