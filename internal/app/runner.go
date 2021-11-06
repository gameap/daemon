package app

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/server"
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
		)
		if err != nil {
			return err
		}

		return srv.Run(ctx)
	}
}

func (r *runner) runGDaemonTaskScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		taskManager := gdaemon_scheduler.NewTaskManager(
			r.container.Get(gdaemonTasksRepositoryDef).(domain.GDTaskRepository),
			r.container.Get(cacheManagerDef).(interfaces.Cache),
			r.container.Get(serverCommandFactoryDef).(*game_server_commands.ServerCommandFactory),
			cfg,
		)

		return taskManager.Run(ctx)
	}
}
