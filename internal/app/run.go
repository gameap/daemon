package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gameap/daemon/internal/app/build"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/di"
	"github.com/gameap/daemon/internal/app/domain"
	loggerpkg "github.com/gameap/daemon/pkg/logger"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

func Run(args []string) {
	app := &cli.App{
		Name:  "gameap-daemon",
		Usage: "GameAP Daemon",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Value:   "",
				Usage:   "Path to gameap-daemon config",
				Aliases: []string{"c"},
			},
		},
		Action: initialize,
		Commands: []*cli.Command{
			{
				Name:  "enroll",
				Usage: "Enroll this daemon with the GameAP panel",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "connect",
						Usage:    "Connect URL (grpc://host:port/setupKey)",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "config-path",
						Value: "/etc/gameap-daemon/gameap-daemon.yaml",
						Usage: "Path to write config file",
					},
					&cli.StringFlag{
						Name:  "certs-dir",
						Value: "/etc/gameap-daemon/certs",
						Usage: "Directory to save TLS certificates",
					},
					&cli.StringFlag{
						Name:  "listen-ip",
						Value: "0.0.0.0",
						Usage: "IP address the daemon listens on",
					},
					&cli.IntFlag{
						Name:  "listen-port",
						Value: 31717,
						Usage: "Port the daemon listens on",
					},
					&cli.StringFlag{
						Name:  "work-path",
						Value: "/srv/gameap",
						Usage: "Working directory for game servers",
					},
				},
				Action: enrollAction,
			},
		},
	}

	err := app.Run(args)
	if err != nil {
		log.Fatal(err)
	}
}

func initialize(c *cli.Context) error {
	domain.StartTime = time.Now()

	log.Infof("GameAP Daemon version: %s", build.Version)
	log.Infof("Build Date: %s", build.BuildDate)

	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return err
	}

	err = loggerpkg.Load(*cfg)
	if err != nil {
		return err
	}

	log.Info("Starting...")

	ctx := shutdownContext(c.Context)
	logger := loggerpkg.NewLogger(*cfg)
	ctx = loggerpkg.WithLogger(ctx, logger)

	container, err := di.NewContainer(cfg, logger)
	if err != nil {
		return err
	}

	processRunner, err := container.ProcessRunner(ctx)
	if err != nil {
		return err
	}

	err = processRunner.Init(ctx, cfg)
	if err != nil {
		return err
	}

	group, ctx := errgroup.WithContext(ctx)

	if cfg.GRPC.Enabled {
		connectionManager, err := container.ConnectionManager(ctx)
		if err != nil {
			return err
		}

		statusReporter, err := container.ServerStatusReporter(ctx)
		if err != nil {
			return err
		}

		processRunner.SetGRPCComponents(connectionManager, statusReporter)
		processRunner.EnableGRPCMode()

		group.Go(processRunner.RunGRPCClient(ctx, cfg))
		group.Go(processRunner.RunGDaemonTaskScheduler(ctx, cfg))
		group.Go(processRunner.RunServersLoopWithReporter(ctx, cfg))
		group.Go(processRunner.RunServerScheduler(ctx, cfg))

		log.Info("Running in gRPC mode")
	} else {
		group.Go(processRunner.RunGDaemonServer(ctx, cfg))
		group.Go(processRunner.RunGDaemonTaskScheduler(ctx, cfg))
		group.Go(processRunner.RunServersLoop(ctx, cfg))
		group.Go(processRunner.RunServerScheduler(ctx, cfg))

		log.Info("Running in legacy mode")
	}

	err = group.Wait()
	if err != nil {
		return err
	}

	return nil
}

func shutdownContext(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGQUIT, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info("Shutdown signal received...")
		cancel()
	}()

	return ctx
}
