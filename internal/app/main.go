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
	appLogger "github.com/gameap/daemon/internal/app/logger"
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

	err = appLogger.Load(*cfg)
	if err != nil {
		return err
	}

	log.Info("Starting...")

	ctx := shutdownContext(context.Background())
	logger := appLogger.NewLogger(*cfg)
	ctx = appLogger.WithLogger(ctx, logger)

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

	group.Go(processRunner.RunGDaemonServer(ctx, cfg))
	group.Go(processRunner.RunGDaemonTaskScheduler(ctx, cfg))
	group.Go(processRunner.RunServersLoop(ctx, cfg))
	group.Go(processRunner.RunServerScheduler(ctx, cfg))

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
