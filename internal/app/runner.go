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
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/pkg/errors"
	"github.com/sarulabs/di"
	log "github.com/sirupsen/logrus"
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
			r.container.Get(gdTaskMangerDef).(domain.GDTaskStatsReader),
		)
		if err != nil {
			return err
		}

		return runService(ctx, srv.Run)
	}
}

func (r *runner) runGDaemonTaskScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		taskManager := r.container.Get(gdTaskMangerDef).(*gdaemon_scheduler.TaskManager)

		return runService(ctx, taskManager.Run)
	}
}

func (r *runner) runServersLoop(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		loop := servers_loop.NewServersLoop(
			r.container.Get(serverRepositoryDef).(domain.ServerRepository),
			r.container.Get(serverCommandFactoryDef).(*game_server_commands.ServerCommandFactory),
			cfg,
		)

		return runService(ctx, loop.Run)
	}
}

func (r *runner) runServerScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		scheduler := serversscheduler.NewScheduler(
			cfg,
			r.container.Get(serverTaskRepositoryDef).(domain.ServerTaskRepository),
			r.container.Get(serverCommandFactoryDef).(*game_server_commands.ServerCommandFactory),
		)

		return runService(ctx, scheduler.Run)
	}
}

func runService(ctx context.Context, runFunc func(ctx context.Context) error) error {
	for {
		select {
		case <-(ctx).Done():
			return nil
		default:
			err := runFunc(ctx)
			if err != nil {
				_, cancel := context.WithCancel(ctx)
				defer cancel()

				log.Error(errors.WithMessage(err, "service stopped unexpectedly with an error"))

				return err
			}
		}
	}
}
