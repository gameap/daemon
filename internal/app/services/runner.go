package services

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	grpcclient "github.com/gameap/daemon/internal/app/grpc"
	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gameap/daemon/internal/app/server"
	serversloop "github.com/gameap/daemon/internal/app/servers_loop"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Runner struct {
	cfg *config.Config

	executor             contracts.Executor
	commandFactory       *gameservercommands.ServerCommandFactory
	apiClient            contracts.APIRequestMaker
	gdTaskManager        *gdaemonscheduler.TaskManager
	serverRepository     domain.ServerRepository
	serverTaskRepository domain.ServerTaskRepository
	connectionManager    *grpcclient.ConnectionManager
	statusReporter       *grpcclient.ServerStatusReporter
	grpcMode             bool
}

func NewProcessRunner(
	cfg *config.Config,
	executor contracts.Executor,
	commandFactory *gameservercommands.ServerCommandFactory,
	apiClient contracts.APIRequestMaker,
	gdTaskManager *gdaemonscheduler.TaskManager,
	serverRepository domain.ServerRepository,
	serverTaskRepository domain.ServerTaskRepository,
) (*Runner, error) {
	return &Runner{
		cfg:                  cfg,
		executor:             executor,
		commandFactory:       commandFactory,
		apiClient:            apiClient,
		gdTaskManager:        gdTaskManager,
		serverRepository:     serverRepository,
		serverTaskRepository: serverTaskRepository,
	}, nil
}

func (r *Runner) SetGRPCComponents(
	connectionManager *grpcclient.ConnectionManager,
	statusReporter *grpcclient.ServerStatusReporter,
) {
	r.connectionManager = connectionManager
	r.statusReporter = statusReporter
}

func (r *Runner) EnableGRPCMode() {
	r.grpcMode = true

	r.gdTaskManager.SetGRPCMode(true)

	if repo, ok := r.serverRepository.(*repositories.ServerRepository); ok {
		repo.SetGRPCMode(true)
	}
}

func (r *Runner) Init(ctx context.Context, cfg *config.Config) error {
	if !cfg.GRPC.Enabled {
		err := r.initNodeConfigFromAPI(ctx, cfg)
		if err != nil {
			return err
		}
	}

	config.InitDefaultScripts(cfg)

	if err := config.UpdateEnvPath(cfg); err != nil {
		log.WithError(err).Warn("Failed to update PATH with tools directories")
	}

	return nil
}

func (r *Runner) initNodeConfigFromAPI(ctx context.Context, cfg *config.Config) error {
	cfgInitializer := config.NewNodeConfigInitializer(r.apiClient)

	return cfgInitializer.Initialize(ctx, cfg)
}

func (r *Runner) RunGDaemonServer(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		certPEM, err := cfg.CertificateChainPEM()
		if err != nil {
			return errors.Wrap(err, "failed to read certificate chain")
		}

		keyPEM, err := cfg.PrivateKeyPEM()
		if err != nil {
			return errors.Wrap(err, "failed to read private key")
		}

		srv, err := server.NewServer(
			cfg.ListenIP,
			cfg.ListenPort,
			certPEM,
			keyPEM,
			server.CredentialsConfig{
				PasswordAuthentication: cfg.PasswordAuthentication,
				Login:                  cfg.DaemonLogin,
				Password:               cfg.DaemonPassword,
			},
			r.executor,
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

		if r.grpcMode {
			scheduler.SetGRPCMode(true)
		}

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "server tasks scheduler",
		}))

		log.Trace("Running server tasks scheduler...")
		return runService(ctx, scheduler.Run)
	}
}

func (r *Runner) RunGRPCClient(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		if !cfg.GRPC.Enabled {
			return nil
		}

		if r.connectionManager == nil {
			return errors.New("gRPC connection manager not initialized")
		}

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "grpc client",
		}))

		log.Info("Running gRPC client...")
		return r.connectionManager.Run(ctx)
	}
}

func (r *Runner) RunServersLoopWithReporter(ctx context.Context, cfg *config.Config) func() error {
	return func() error {
		loop := serversloop.NewServersLoop(r.serverRepository, r.commandFactory, cfg)

		if r.statusReporter != nil {
			loop.SetStatusReporter(r.statusReporter)
			r.statusReporter.Start(ctx)
		}

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "servers loop",
		}))

		log.Trace("Running server loop...")
		return runService(ctx, loop.Run)
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
				log.Error(errors.WithMessage(err, "service stopped unexpectedly with an error"))

				return err
			}
		}
	}
}
