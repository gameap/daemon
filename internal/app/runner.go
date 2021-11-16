package app

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/logger"
	"github.com/gameap/daemon/internal/app/server"
	serversloop "github.com/gameap/daemon/internal/app/servers_loop"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/pkg/errors"
	"github.com/sarulabs/di"
	log "github.com/sirupsen/logrus"
)

type runner struct {
	container di.Container
}

func newProcessManager(cfg *config.Config, logger *log.Logger) (*runner, error) {
	builder, err := NewBuilder(cfg, logger)
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

		ctx = logger.WithLogger(ctx, logger.WithFields(ctx, log.Fields{
			"service": "gameap daemon server",
		}))

		log.Trace("Running gameap damon server...")
		return runService(ctx, srv.Run)
	}
}

func (r *runner) runGDaemonTaskScheduler(ctx context.Context, _ *config.Config) func() error {
	return func() error {
		taskManager := r.container.Get(gdTaskMangerDef).(*gdaemonscheduler.TaskManager)

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "gdtask scheduler",
		}))

		log.Trace("Running gdtask scheduler...")
		return runService(ctx, taskManager.Run)
	}
}

func (r *runner) runServersLoop(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		loop := serversloop.NewServersLoop(
			r.container.Get(serverRepositoryDef).(domain.ServerRepository),
			r.container.Get(serverCommandFactoryDef).(*gameservercommands.ServerCommandFactory),
			cfg,
		)

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "servers loop",
		}))

		log.Trace("Running server loop...")
		return runService(ctx, loop.Run)
	}
}

func (r *runner) runServerScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		scheduler := serversscheduler.NewScheduler(
			cfg,
			r.container.Get(serverTaskRepositoryDef).(domain.ServerTaskRepository),
			r.container.Get(serverCommandFactoryDef).(*gameservercommands.ServerCommandFactory),
		)

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "server tasks scheduler",
		}))

		log.Trace("Running server tasks scheduler...")
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
