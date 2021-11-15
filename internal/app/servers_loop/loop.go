package serversloop

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	commands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/logger"
	log "github.com/sirupsen/logrus"
)

const (
	commandTimeout = 10 * time.Second
	loopDuration   = 30 * time.Second
)

type ServersLoop struct {
	cfg                  *config.Config
	serverRepo           domain.ServerRepository
	serverCommandFactory *commands.ServerCommandFactory
}

func NewServersLoop(
	serverRepo domain.ServerRepository,
	serverCommandFactory *commands.ServerCommandFactory,
	cfg *config.Config,
) *ServersLoop {
	return &ServersLoop{
		cfg,
		serverRepo,
		serverCommandFactory,
	}
}

func (l *ServersLoop) Run(ctx context.Context) error {
	return l.loop(ctx)
}

func (l *ServersLoop) loop(ctx context.Context) error {
	ticker := time.NewTicker(loopDuration)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			l.tick(ctx)
		}
	}
}

func (l *ServersLoop) tick(ctx context.Context) {
	ids, err := l.serverRepo.IDs(ctx)
	if err != nil {
		log.Error(err)
		return
	}

	for i := range ids {
		server, err := l.serverRepo.FindByID(ctx, ids[i])
		if err != nil {
			logger.Error(ctx, err)
			continue
		}

		ctxWithServer := logger.WithLogger(ctx, logger.WithField(ctx, "gameServerID", server.ID()))

		err = l.pipeline(ctxWithServer, server, []pipelineHandler{l.checkStatus, l.startIfNeeded})
		if err != nil {
			logger.Error(ctx, err)
			continue
		}
	}
}

type pipelineHandler func(ctx context.Context, server *domain.Server) error

func (l *ServersLoop) pipeline(ctx context.Context, server *domain.Server, handlers []pipelineHandler) error {
	for _, h := range handlers {
		err := h(ctx, server)
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *ServersLoop) checkStatus(ctx context.Context, server *domain.Server) error {
	if server.InstallationStatus() != domain.ServerInstalled {
		return nil
	}

	statusCmd := l.serverCommandFactory.LoadServerCommandFunc(commands.Status)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	err := statusCmd.Execute(ctxWithTimeout, server)
	if err != nil {
		return err
	}

	server.SetStatus(statusCmd.Result() == commands.SuccessResult)
	err = l.serverRepo.Save(ctx, server)
	if err != nil {
		return err
	}

	return nil
}

func (l *ServersLoop) startIfNeeded(ctx context.Context, server *domain.Server) error {
	if server.InstallationStatus() != domain.ServerInstalled {
		return nil
	}

	if server.IsActive() || !server.AutoStart() {
		return nil
	}

	statusCmd := l.serverCommandFactory.LoadServerCommandFunc(commands.Start)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	err := statusCmd.Execute(ctxWithTimeout, server)
	if err != nil {
		return err
	}

	return nil
}
