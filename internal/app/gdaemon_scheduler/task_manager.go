package gdaemonscheduler

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
)

var updateTimeout = 5 * time.Second

var taskServerCommandMap = map[domain.GDTaskCommand]domain.ServerCommand{
	domain.GDTaskGameServerStart:     domain.Start,
	domain.GDTaskGameServerPause:     domain.Pause,
	domain.GDTaskGameServerStop:      domain.Stop,
	domain.GDTaskGameServerKill:      domain.Kill,
	domain.GDTaskGameServerRestart:   domain.Restart,
	domain.GDTaskGameServerInstall:   domain.Install,
	domain.GDTaskGameServerReinstall: domain.Reinstall,
	domain.GDTaskGameServerUpdate:    domain.Update,
	domain.GDTaskGameServerDelete:    domain.Delete,
}

type TaskManager struct {
	config               *config.Config
	repository           domain.GDTaskRepository
	executor             contracts.Executor
	serverCommandFactory *gameservercommands.ServerCommandFactory

	// Runtime
	mutex              *sync.Mutex
	lastUpdated        time.Time
	commandsInProgress sync.Map // map[domain.GDTask]contracts.CommandResultReader
	queue              *taskQueue
	cache              contracts.Cache
}

func NewTaskManager(
	repository domain.GDTaskRepository,
	cache contracts.Cache,
	serverCommandFactory *gameservercommands.ServerCommandFactory,
	executor contracts.Executor,
	config *config.Config,
) *TaskManager {
	return &TaskManager{
		config:               config,
		repository:           repository,
		cache:                cache,
		queue:                newTaskQueue(),
		serverCommandFactory: serverCommandFactory,
		mutex:                &sync.Mutex{},
		executor:             executor,
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

			time.Sleep(1 * time.Second)

			err = manager.updateTasksIfNeeded(ctx)
			if err != nil {
				logger.Logger(ctx).Error(err)
			}
		}
	}
}

func (manager *TaskManager) Stats() domain.GDTaskStats {
	stats := domain.GDTaskStats{}

	manager.commandsInProgress.Range(func(key, value interface{}) bool {
		stats.WorkingCount++
		return true
	})

	stats.WaitingCount = manager.queue.Len() - stats.WorkingCount

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

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithField("gdTaskID", task.ID()))

	if task.Server() != nil {
		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithField("gameServerID", task.Server().ID()))
	}

	if manager.shouldTaskWaitForAnotherToComplete(task) {
		return
	}

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

		if task.Server() != nil {
			task.Server().NoticeTaskCompleted()
		}

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

	if task.Task() == domain.GDTaskCommandExecute {
		return manager.executeCommand(ctx, task)
	}

	return manager.executeGameCommand(ctx, task)
}

func (manager *TaskManager) executeCommand(ctx context.Context, task *domain.GDTask) error {
	cmd := newExecuteCommand(manager.config, manager.executor)

	manager.commandsInProgress.Store(*task, cmd)

	logger.Debug(ctx, "Running task command")

	go func() {
		err := cmd.Execute(ctx, task.Command(), contracts.ExecutorOptions{
			WorkDir: manager.config.WorkDir(),
		})

		if err != nil {
			logger.Warn(ctx, err)
			manager.appendTaskOutput(ctx, task, []byte(err.Error()))
			manager.failTask(ctx, task)
		}
	}()

	return nil
}

func (manager *TaskManager) executeGameCommand(ctx context.Context, task *domain.GDTask) error {
	cmd, gameServerCmdExist := taskServerCommandMap[task.Task()]

	if !gameServerCmdExist {
		return ErrInvalidTaskError
	}

	cmdFunc := manager.serverCommandFactory.LoadServerCommand(cmd, task.Server())

	manager.commandsInProgress.Store(*task, cmdFunc)

	logger.Debug(ctx, "Running task command")

	go func() {
		err := cmdFunc.Execute(ctx, task.Server())
		if err != nil {
			logger.Warn(ctx, err)
			manager.appendTaskOutput(
				ctx,
				task,
				append(cmdFunc.ReadOutput(), err.Error()...),
			)
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

	cmd := c.(contracts.CommandResultReader)

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
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

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
	mutex *sync.Mutex
}

func newTaskQueue() *taskQueue {
	return &taskQueue{
		mutex: &sync.Mutex{},
	}
}

func (q *taskQueue) Insert(tasks []*domain.GDTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	for _, t := range tasks {
		existenceTask := q.FindByID(t.ID())
		if existenceTask == nil {
			q.tasks = append(q.tasks, t)
		}
	}
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
	if len(q.tasks) == 0 {
		return
	}

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

type executeCommand struct {
	config   *config.Config
	output   io.ReadWriter
	mu       *sync.Mutex
	complete bool
	result   int

	executor contracts.Executor
}

func newExecuteCommand(config *config.Config, executor contracts.Executor) *executeCommand {
	return &executeCommand{
		config:   config,
		executor: executor,
		output:   components.NewSafeBuffer(),
		mu:       &sync.Mutex{},
	}
}

func (e *executeCommand) Execute(
	ctx context.Context,
	command string,
	options contracts.ExecutorOptions,
) error {
	command = strings.ReplaceAll(command, "{node_work_path}", e.config.WorkPath)
	command = strings.ReplaceAll(command, "{node_tools_path}", e.config.WorkPath+"/tools")

	result, err := e.executor.ExecWithWriter(ctx, command, e.output, options)

	e.mu.Lock()
	defer e.mu.Unlock()

	e.result = result
	e.complete = true

	return err
}

func (e *executeCommand) ReadOutput() []byte {
	out, err := io.ReadAll(e.output)
	if err != nil {
		return nil
	}

	return out
}

func (e *executeCommand) Result() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.result
}

func (e *executeCommand) IsComplete() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.complete
}
