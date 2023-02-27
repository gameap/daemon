package serversloop

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	commands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
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
		ctxWithServer := logger.WithLogger(ctx, logger.WithField(ctx, "gameServerID", ids[i]))

		server, err := l.serverRepo.FindByID(ctxWithServer, ids[i])
		if err != nil {
			logger.Error(ctxWithServer, err)
			continue
		}

		err = l.pipeline(ctxWithServer, server, []pipelineHandler{
			l.checkStatus,
			l.startIfNeeded,
			l.save,
		})
		if err != nil {
			logger.Error(ctxWithServer, err)
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

	statusCmd := l.serverCommandFactory.LoadServerCommand(domain.Status, server)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	err := statusCmd.Execute(ctxWithTimeout, server)
	if err != nil {
		return errors.WithMessage(err, "failed to execute status command")
	}

	server.SetStatus(statusCmd.Result() == commands.SuccessResult)

	return nil
}

func (l *ServersLoop) startIfNeeded(ctx context.Context, server *domain.Server) error {
	if server.InstallationStatus() != domain.ServerInstalled {
		return nil
	}

	if server.IsActive() || !server.AutoStart() {
		return nil
	}

	startCMD := l.serverCommandFactory.LoadServerCommand(domain.Start, server)

	ctxWithTimeout, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	err := startCMD.Execute(ctxWithTimeout, server)
	if err != nil {
		return errors.WithMessage(err, "failed to execute start command")
	}

	return l.checkStatus(ctx, server)
}

func (l *ServersLoop) save(ctx context.Context, server *domain.Server) error {
	if server.InstallationStatus() != domain.ServerInstalled {
		return nil
	}

	return l.serverRepo.Save(ctx, server)
}
