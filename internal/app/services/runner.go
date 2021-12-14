package services

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/server"
	serversloop "github.com/gameap/daemon/internal/app/servers_loop"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Runner struct {
	cfg *config.Config

	commandFactory       *gameservercommands.ServerCommandFactory
	apiClient            contracts.APIRequestMaker
	gdTaskManager        *gdaemonscheduler.TaskManager
	serverRepository     domain.ServerRepository
	serverTaskRepository domain.ServerTaskRepository
}

func NewProcessRunner(
	cfg *config.Config,
	commandFactory *gameservercommands.ServerCommandFactory,
	apiClient contracts.APIRequestMaker,
	gdTaskManager *gdaemonscheduler.TaskManager,
	serverRepository domain.ServerRepository,
	serverTaskRepository domain.ServerTaskRepository,
) (*Runner, error) {
	return &Runner{
		cfg:                  cfg,
		commandFactory:       commandFactory,
		apiClient:            apiClient,
		gdTaskManager:        gdTaskManager,
		serverRepository:     serverRepository,
		serverTaskRepository: serverTaskRepository,
	}, nil
}

func (r *Runner) Init(ctx context.Context, cfg *config.Config) error {
	err := r.initNodeConfigFromAPI(ctx, cfg)
	if err != nil {
		return err
	}

	return nil
}

func (r *Runner) initNodeConfigFromAPI(ctx context.Context, cfg *config.Config) error {
	cfgInitializer := config.NewNodeConfigInitializer(r.apiClient)
	return cfgInitializer.Initialize(ctx, cfg)
}

func (r *Runner) RunGDaemonServer(ctx context.Context, cfg *config.Config) func() error {
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
			r.gdTaskManager,
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

func (r *Runner) RunGDaemonTaskScheduler(ctx context.Context, _ *config.Config) func() error {
	return func() error {
		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "gdtask scheduler",
		}))

		log.Trace("Running gdtask scheduler...")
		return runService(ctx, r.gdTaskManager.Run)
	}
}

func (r *Runner) RunServersLoop(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		loop := serversloop.NewServersLoop(r.serverRepository, r.commandFactory, cfg)

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "servers loop",
		}))

		log.Trace("Running server loop...")
		return runService(ctx, loop.Run)
	}
}

func (r *Runner) RunServerScheduler(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		scheduler := serversscheduler.NewScheduler(
			cfg,
			r.serverTaskRepository,
			r.commandFactory,
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
