package serversloop

import (
	"context"
	"sync"
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
	loopDuration   = 5 * time.Second

	// Ticker will not skip the server if the server performed the task less than this time
	noSkipTime = 5 * time.Minute

	// Maximum number of skips if the server performed the task less than 10 minutes ago
	skipMaxCount10m = 3

	// Maximum number of skips if the server performed the task less than 60 minutes ago
	skipMaxCount60m = 10

	// Maximum number of skips if the server performed the task more than 60 minutes ago
	skipMaxCount = 20
)

type ServersLoop struct {
	cfg                  *config.Config
	serverRepo           domain.ServerRepository
	serverCommandFactory *commands.ServerCommandFactory

	skipCounter skipCounter
}

func NewServersLoop(
	serverRepo domain.ServerRepository,
	serverCommandFactory *commands.ServerCommandFactory,
	cfg *config.Config,
) *ServersLoop {
	return &ServersLoop{
		cfg:                  cfg,
		serverRepo:           serverRepo,
		serverCommandFactory: serverCommandFactory,

		skipCounter: skipCounter{},
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

		if l.canSkipped(ctxWithServer, server) {
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

func (l *ServersLoop) canSkipped(_ context.Context, server *domain.Server) bool {
	if time.Since(server.LastTaskCompletedAt()) <= noSkipTime {
		return false
	}

	if time.Since(server.LastTaskCompletedAt()) <= 10*time.Minute {
		if l.skipCounter.Get(server.ID()) >= skipMaxCount10m {
			l.skipCounter.Reset(server.ID())
			return false
		}

		l.skipCounter.Increment(server.ID())
		return true
	}

	if time.Since(server.LastTaskCompletedAt()) <= 60*time.Minute {
		if l.skipCounter.Get(server.ID()) >= skipMaxCount60m {
			l.skipCounter.Reset(server.ID())
			return false
		}

		l.skipCounter.Increment(server.ID())
		return true
	}

	if l.skipCounter.Get(server.ID()) >= skipMaxCount {
		l.skipCounter.Reset(server.ID())
		return false
	}

	l.skipCounter.Increment(server.ID())
	return true
}

type skipCounter struct {
	counter sync.Map
}

func (sc *skipCounter) Increment(serverID int) {
	val, ok := sc.counter.Load(serverID)
	if !ok {
		sc.counter.Store(serverID, 1)
		return
	}

	sc.counter.Store(serverID, val.(int)+1)
}

func (sc *skipCounter) Reset(serverID int) {
	sc.counter.Delete(serverID)
}

func (sc *skipCounter) Get(serverID int) int {
	val, ok := sc.counter.Load(serverID)
	if !ok {
		return 0
	}

	return val.(int)
}
