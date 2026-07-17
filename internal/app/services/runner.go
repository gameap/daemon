package services

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	grpcclient "github.com/gameap/daemon/internal/app/grpc"
	serversloop "github.com/gameap/daemon/internal/app/servers_loop"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type Runner struct {
	cfg *config.Config

	commandFactory    *gameservercommands.ServerCommandFactory
	gdTaskManager     *gdaemonscheduler.TaskManager
	serverRepository  domain.ServerRepository
	serversScheduler  *serversscheduler.Scheduler
	connectionManager *grpcclient.ConnectionManager
	statusReporter    *grpcclient.ServerStatusReporter
}

func NewProcessRunner(
	cfg *config.Config,
	commandFactory *gameservercommands.ServerCommandFactory,
	gdTaskManager *gdaemonscheduler.TaskManager,
	serverRepository domain.ServerRepository,
) (*Runner, error) {
	return &Runner{
		cfg:              cfg,
		commandFactory:   commandFactory,
		gdTaskManager:    gdTaskManager,
		serverRepository: serverRepository,
	}, nil
}

func (r *Runner) SetServersScheduler(scheduler *serversscheduler.Scheduler) {
	r.serversScheduler = scheduler
}

func (r *Runner) SetGRPCComponents(
	connectionManager *grpcclient.ConnectionManager,
	statusReporter *grpcclient.ServerStatusReporter,
) {
	r.connectionManager = connectionManager
	r.statusReporter = statusReporter
}

func (r *Runner) Init(_ context.Context, cfg *config.Config) error {
	config.InitDefaultScripts(cfg)

	if err := config.UpdateEnvPath(cfg); err != nil {
		log.WithError(err).Warn("Failed to update PATH with tools directories")
	}

	return nil
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

func (r *Runner) RunServerScheduler(ctx context.Context, _ *config.Config) func() error {
	return func() error {
		if r.serversScheduler == nil {
			return errors.New("servers scheduler not wired")
		}

		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
			"service": "server tasks scheduler",
		}))

		log.Trace("Running server tasks scheduler...")
		return runService(ctx, r.serversScheduler.Run)
	}
}

func (r *Runner) RunGRPCClient(ctx context.Context, _ *config.Config) func() error {
	return func() error {
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

func (r *Runner) RunServersLoop(ctx context.Context, cfg *config.Config) func() error {
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
