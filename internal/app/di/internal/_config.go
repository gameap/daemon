package internal

import (
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/go-resty/resty/v2"
	"github.com/sirupsen/logrus"
)

// Container is a root dependency injection container. It is required to describe
// your services.
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
	resty     *resty.Client
	apiCaller contracts.APIRequestMaker `di:"set"`
	executor  contracts.Executor

	gdTaskManager *gdaemonscheduler.TaskManager
}

type RepositoryContainer struct {
	gdTaskRepository     domain.GDTaskRepository     `di:"public, set"`
	serverRepository     domain.ServerRepository     `di:"public, set"`
	serverTaskRepository domain.ServerTaskRepository `di:"public, set"`
}
