package gdaemonscheduler

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var updateTimeout = 5 * time.Second

var taskServerCommandMap = map[domain.GDTaskCommand]gameservercommands.ServerCommand{
	domain.GDTaskGameServerStart:     gameservercommands.Start,
	domain.GDTaskGameServerPause:     gameservercommands.Pause,
	domain.GDTaskGameServerStop:      gameservercommands.Stop,
	domain.GDTaskGameServerKill:      gameservercommands.Kill,
	domain.GDTaskGameServerRestart:   gameservercommands.Restart,
	domain.GDTaskGameServerInstall:   gameservercommands.Install,
	domain.GDTaskGameServerReinstall: gameservercommands.Reinstall,
	domain.GDTaskGameServerUpdate:    gameservercommands.Update,
	domain.GDTaskGameServerDelete:    gameservercommands.Delete,
}

type TaskManager struct {
	config               *config.Config
	repository           domain.GDTaskRepository
	serverCommandFactory *gameservercommands.ServerCommandFactory

	// Runtime
	lastUpdated        time.Time
	commandsInProgress sync.Map // map[domain.GDTask]interfaces.Command
	queue              taskQueue
	cache              interfaces.Cache
}

func NewTaskManager(
	repository domain.GDTaskRepository,
	cache interfaces.Cache,
	serverCommandFactory *gameservercommands.ServerCommandFactory,
	config *config.Config,
) *TaskManager {
	return &TaskManager{
		config:               config,
		repository:           repository,
		cache:                cache,
		queue:                taskQueue{},
		serverCommandFactory: serverCommandFactory,
	}
}

func (manager *TaskManager) Run(ctx context.Context) error {
	manager.failWorkingTaskAfterRestart(ctx)

	err := manager.updateTasksIfNeeded(ctx)
	if err != nil {
		logger.Logger(ctx).Error(err)
	}

	for {
		select {
		case <-(ctx).Done():
			return nil
		default:
			manager.runNext(ctx)

			err = manager.updateTasksIfNeeded(ctx)
			if err != nil {
				logger.Logger(ctx).Error(err)
			}

			time.Sleep(5 * time.Second)
		}
	}
}

func (manager *TaskManager) Stats() domain.GDTaskStats {
	stats := domain.GDTaskStats{}

	manager.commandsInProgress.Range(func(key, value interface{}) bool {
		stats.WorkingCount++
		return true
	})

	stats.WaitingCount = manager.queue.Len()

	return stats
}

func (manager *TaskManager) failWorkingTaskAfterRestart(ctx context.Context) {
	workingTasks, err := manager.repository.FindByStatus(ctx, domain.GDTaskStatusWorking)
	if err != nil {
		logger.Logger(ctx).Error(err)
	}

	for _, task := range workingTasks {
		err = task.SetStatus(domain.GDTaskStatusError)
		if err != nil {
			logger.Logger(ctx).Error(err)
			continue
		}

		manager.appendTaskOutput(ctx, task, []byte("Working task failed. GameAP Daemon was restarted."))
		err = manager.repository.Save(ctx, task)
		if err != nil {
			logger.Logger(ctx).Error(err)
		}
	}
}

func (manager *TaskManager) runNext(ctx context.Context) {
	task := manager.queue.Next()
	if task == nil {
		return
	}

	if manager.shouldTaskWaitForAnotherToComplete(task) {
		return
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(log.Fields{
		"gdTaskID":     task.ID(),
		"gameServerID": task.Server().ID(),
	}))

	var err error
	if task.IsWaiting() {
		err = manager.executeTask(ctx, task)
	} else if task.IsWorking() {
		err = manager.proceedTask(ctx, task)
	}

	if err != nil {
		logger.Logger(ctx).WithError(err).Error("task execution failed")

		manager.appendTaskOutput(ctx, task, []byte(err.Error()))
		manager.failTask(ctx, task)
	}

	if task.IsComplete() {
		logger.Debug(ctx, "Task completed")

		manager.queue.Remove(task)

		err = manager.repository.Save(ctx, task)
		if err != nil {
			err = errors.WithMessage(err, "[gdaemon_scheduler.TaskManager] failed to save task")
			logger.Error(ctx, err)
		}
	}
}

func (manager *TaskManager) shouldTaskWaitForAnotherToComplete(task *domain.GDTask) bool {
	if task.RunAfterID() > 0 {
		t := manager.queue.FindByID(task.RunAfterID())

		if t == nil {
			return false
		}

		if !t.IsComplete() {
			return true
		}
	}

	return false
}

func (manager *TaskManager) executeTask(ctx context.Context, task *domain.GDTask) error {
	err := task.SetStatus(domain.GDTaskStatusWorking)
	if err != nil {
		return err
	}

	err = manager.repository.Save(ctx, task)
	if err != nil {
		err = errors.WithMessage(err, "[gdaemon_scheduler.TaskManager] failed to save task")
		logger.Error(ctx, err)
	}

	cmd, gameServerCmdExist := taskServerCommandMap[task.Task()]

	if !gameServerCmdExist {
		return ErrInvalidTaskError
	}

	cmdFunc := manager.serverCommandFactory.LoadServerCommandFunc(cmd)

	manager.commandsInProgress.Store(*task, cmdFunc)

	logger.Debug(ctx, "Running task command")

	go func() {
		err = cmdFunc.Execute(ctx, task.Server())
		if err != nil {
			logger.Warn(ctx, err)
			manager.appendTaskOutput(ctx, task, []byte(err.Error()))
			manager.failTask(ctx, task)
		}
	}()

	return nil
}

func (manager *TaskManager) proceedTask(ctx context.Context, task *domain.GDTask) error {
	c, ok := manager.commandsInProgress.Load(*task)
	if !ok {
		return errors.New("[gdaemon_scheduler.TaskManager] task not exist in working tasks")
	}

	cmd := c.(interfaces.Command)

	if cmd.IsComplete() {
		if cmd.Result() == gameservercommands.SuccessResult {
			manager.commandsInProgress.Delete(*task)
			err := task.SetStatus(domain.GDTaskStatusSuccess)
			if err != nil {
				return err
			}
		} else {
			manager.commandsInProgress.Delete(*task)
			manager.failTask(ctx, task)
		}
	}

	manager.appendTaskOutput(ctx, task, cmd.ReadOutput())

	return nil
}

func (manager *TaskManager) failTask(ctx context.Context, task *domain.GDTask) {
	err := task.SetStatus(domain.GDTaskStatusError)
	if err != nil {
		logger.Error(ctx, err)
	}
}

func (manager *TaskManager) appendTaskOutput(ctx context.Context, task *domain.GDTask, output []byte) {
	if len(output) == 0 {
		return
	}

	err := manager.repository.AppendOutput(ctx, task, output)
	if err != nil {
		logger.Logger(ctx).Error(err)
	}
}

func (manager *TaskManager) updateTasksIfNeeded(ctx context.Context) error {
	if time.Since(manager.lastUpdated) <= updateTimeout {
		return nil
	}

	tasks, err := manager.repository.FindByStatus(ctx, domain.GDTaskStatusWaiting)
	if err != nil {
		return err
	}

	if len(tasks) > 0 {
		manager.queue.Insert(tasks)
	}

	manager.lastUpdated = time.Now()

	return nil
}

type taskQueue struct {
	tasks []*domain.GDTask
	mutex sync.Mutex
}

func (q *taskQueue) Insert(tasks []*domain.GDTask) {
	q.mutex.Lock()
	for _, t := range tasks {
		existenceTask := q.FindByID(t.ID())
		if existenceTask == nil {
			q.tasks = append(q.tasks, t)
		}
	}

	q.mutex.Unlock()
}

func (q *taskQueue) Dequeue() *domain.GDTask {
	if len(q.tasks) == 0 {
		return nil
	}

	task := q.tasks[0]

	q.mutex.Lock()
	q.tasks = q.tasks[1:]
	q.mutex.Unlock()

	return task
}

func (q *taskQueue) Next() *domain.GDTask {
	if len(q.tasks) == 0 {
		return nil
	}

	task := q.Dequeue()
	q.Insert([]*domain.GDTask{task})

	return task
}

func (q *taskQueue) Remove(task *domain.GDTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	for i := range q.tasks {
		if q.tasks[i].ID() == task.ID() {
			q.tasks[i] = q.tasks[len(q.tasks)-1]
			q.tasks = q.tasks[:len(q.tasks)-1]
		}
	}
}

func (q *taskQueue) FindByID(id int) *domain.GDTask {
	for _, task := range q.tasks {
		if task.ID() == id {
			return task
		}
	}

	return nil
}

func (q *taskQueue) WorkingTasks() ([]int, []*domain.GDTask) {
	ids := make([]int, 0, len(q.tasks))
	tasks := make([]*domain.GDTask, 0, len(q.tasks))

	for _, task := range q.tasks {
		if task.IsWorking() {
			ids = append(ids, task.ID())
			tasks = append(tasks, task)
		}
	}

	return ids, tasks
}

func (q *taskQueue) Len() int {
	return len(q.tasks)
}
