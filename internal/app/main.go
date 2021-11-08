package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/logger"
	"golang.org/x/sync/errgroup"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

func Run(args []string) {
	app := &cli.App{
		Name:  "gameap-daemon",
		Usage: "GameAP Daemon",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Value: "/etc/gameap-daemon/gameap-daemon.cfg",
				Usage: "Path to gameap-daemon config",
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
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return err
	}

	err = logger.Load(*cfg)
	if err != nil {
		return err
	}

	log.Info("Starting...")

	ctx := shutdownContext(context.Background())
	group, ctx := errgroup.WithContext(ctx)

	processManager, err := newProcessManager(cfg)
	if err != nil {
		return err
	}

	err = processManager.init(ctx, cfg)
	if err != nil {
		return err
	}

	group.Go(processManager.runGDaemonServer(ctx, cfg))
	group.Go(processManager.runGDaemonTaskScheduler(ctx, cfg))
	group.Go(processManager.runServersLoop(ctx, cfg))

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
