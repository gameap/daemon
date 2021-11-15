package serversscheduler

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/gameservercommands"
	"github.com/gameap/daemon/internal/app/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var updateTimeout = 5 * time.Second

type Scheduler struct {
	config               *config.Config
	repository           domain.ServerTaskRepository
	serverCommandFactory *gameservercommands.ServerCommandFactory

	// Runtime, state
	lastUpdated time.Time
	queue       *taskQueue
}

func NewScheduler(
	config *config.Config,
	repository domain.ServerTaskRepository,
	serverCommandFactory *gameservercommands.ServerCommandFactory,
) *Scheduler {
	return &Scheduler{
		config:               config,
		repository:           repository,
		serverCommandFactory: serverCommandFactory,
		queue:                newTaskQueue(),
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	err := s.updateTasksIfNeeded(ctx)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warn("Failed to update game server tasks")
	}

	for {
		select {
		case <-(ctx).Done():
			return nil
		default:
			s.runNext(ctx)

			err = s.updateTasksIfNeeded(ctx)
			if err != nil {
				logger.Logger(ctx).WithError(err).Warn("Failed to update game server tasks")
			}

			time.Sleep(updateTimeout)
		}
	}
}

func (s *Scheduler) runNext(ctx context.Context) {
	task := s.queue.Pop()
	if task == nil {
		return
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"serverTaskID": task.ID,
		"gameServerID": task.Server.ID(),
	}))

	if task.ExecuteDate.Before(time.Now()) {
		s.queue.Remove(task)

		if task.CanExecute() {
			s.executeTask(ctx, task)
			s.prolongTask(ctx, task)
		}
	}
}

func (s *Scheduler) executeTask(ctx context.Context, task *domain.ServerTask) {
	cmd := s.serverCommandFactory.LoadServerCommandFunc(taskCommandToServerCommand(task.Command))

	err := cmd.Execute(ctx, task.Server)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warn("Failed to execute server task")
		s.saveFailInfo(ctx, task, err.Error())
		return
	}

	result := cmd.Result()
	if result == gameservercommands.ErrorResult {
		s.saveFailInfo(ctx, task, string(cmd.ReadOutput()))
		return
	}
}

func (s *Scheduler) prolongTask(ctx context.Context, task *domain.ServerTask) {
	task.IncreaseCountersAndTime()

	err := s.repository.Save(ctx, task)
	if err != nil {
		logger.Logger(ctx).WithError(err).Warn("Failed to prolong game server task")
	}

	s.queue.Put(task)
}

func (s *Scheduler) saveFailInfo(ctx context.Context, task *domain.ServerTask, errorText string) {
	err := s.repository.Fail(ctx, task, []byte(errorText))
	if err != nil {
		log.Error(err)
	}
}

func (s *Scheduler) updateTasksIfNeeded(ctx context.Context) error {
	if time.Since(s.lastUpdated) <= updateTimeout {
		return nil
	}

	tasks, err := s.repository.Find(ctx)
	if err != nil {
		return errors.WithMessage(err, "failed to get server tasks")
	}

	for _, t := range tasks {
		if !t.CanExecute() {
			continue
		}

		if s.queue.Exists(t) {
			s.queue.Replace(t)
		}

		s.queue.Put(t)
	}

	s.lastUpdated = time.Now()

	return nil
}

var commandMap = map[domain.ServerTaskCommand]gameservercommands.ServerCommand{
	domain.ServerTaskStart:     gameservercommands.Start,
	domain.ServerTaskStop:      gameservercommands.Stop,
	domain.ServerTaskRestart:   gameservercommands.Restart,
	domain.ServerTaskUpdate:    gameservercommands.Update,
	domain.ServerTaskReinstall: gameservercommands.Reinstall,
}

func taskCommandToServerCommand(cmd domain.ServerTaskCommand) gameservercommands.ServerCommand {
	return commandMap[cmd]
}
