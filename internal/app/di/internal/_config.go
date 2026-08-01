package internal

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/sirupsen/logrus"
)

// Container is a root dependency injection container. It is required to describe
// your services.
//
// NOTE: the generated containers in this package have been hand-maintained
// since the gRPC wiring was added (GameStore, GatewayClient, ConnectionManager,
// servers scheduler). Do not regenerate them with digen — it would drop that
// wiring. This file is kept in sync manually as documentation.
type Container struct {
	cfg    *config.Config `di:"required"`
	logger *logrus.Logger `di:"required"`

	processRunner *services.Runner `di:"public"`

	cacheManager contracts.Cache

	serverCommandFactory *gameservercommands.ServerCommandFactory

	services     ServicesContainer
	repositories RepositoryContainer
}

type ServicesContainer struct {
	executor           contracts.Executor
	extendableExecutor contracts.Executor

	gdTaskManager *gdaemonscheduler.TaskManager
}

type RepositoryContainer struct {
	serverRepository domain.ServerRepository `di:"public"`
}
