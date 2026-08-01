package gdaemonscheduler

import (
	"context"
	"fmt"
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
	log "github.com/sirupsen/logrus"
)

type TaskStatusSender interface {
	SendTaskStatus(taskID int, status string, message string)
	SendTaskOutput(taskID int, output []byte, isFinal bool)
}

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
	executor             contracts.Executor
	cache                contracts.Cache
	config               *config.Config
	serverCommandFactory *gameservercommands.ServerCommandFactory
	queue                *taskQueue
	completed            *completionTracker
	commandsInProgress   sync.Map
	wg                   sync.WaitGroup
	taskStatusSender     TaskStatusSender

	// predecessorWaits maps a task ID to the moment its predecessor was first
	// seen missing, bounding the wait by predecessorMissingTimeout.
	predecessorWaits          sync.Map
	predecessorMissingTimeout time.Duration
}

func NewTaskManager(
	cache contracts.Cache,
	serverCommandFactory *gameservercommands.ServerCommandFactory,
	executor contracts.Executor,
	config *config.Config,
) *TaskManager {
	return &TaskManager{
		config:                    config,
		cache:                     cache,
		queue:                     newTaskQueue(),
		completed:                 newCompletionTracker(completionTrackerCapacity),
		serverCommandFactory:      serverCommandFactory,
		executor:                  executor,
		predecessorMissingTimeout: predecessorMissingTimeout,
	}
}

func (manager *TaskManager) SetTaskStatusSender(sender TaskStatusSender) {
	manager.taskStatusSender = sender
}

func (manager *TaskManager) InsertTask(task *domain.GDTask) {
	manager.queue.Insert([]*domain.GDTask{task})
}

func (manager *TaskManager) CancelTask(taskID int) error {
	task := manager.queue.FindByID(taskID)
	if task == nil {
		return errors.New("task not found")
	}

	if err := task.SetStatus(domain.GDTaskStatusCanceled); err != nil {
		return err
	}

	manager.queue.Remove(task)
	manager.predecessorWaits.Delete(taskID)

	return nil
}

func (manager *TaskManager) Run(ctx context.Context) error {
	go manager.RunWorker(ctx)

	<-ctx.Done()
	manager.wg.Wait()

	return nil
}

func (manager *TaskManager) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if manager.queue.Len() > 0 {
				manager.runNext(ctx)
			}
		}
	}
}

func (manager *TaskManager) WorkingTasks() ([]int, []*domain.GDTask) {
	return manager.queue.WorkingTasks()
}

func (manager *TaskManager) Stats() domain.GDTaskStats {
	stats := domain.GDTaskStats{}

	manager.commandsInProgress.Range(func(_, _ interface{}) bool {
		stats.WorkingCount++
		return true
	})

	stats.WaitingCount = manager.queue.Len() - stats.WorkingCount

	return stats
}

func (manager *TaskManager) runNext(ctx context.Context) {
	task := manager.queue.Next()
	if task == nil {
		return
	}

	ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithFields(
		log.Fields{
			"gdTaskID":      task.ID(),
			"gdTaskCommand": string(task.Task()),
		},
	))

	if task.RunAfterID() > 0 {
		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithField("runAfterTaskID", task.RunAfterID()))
	}

	if task.Server() != nil {
		ctx = logger.WithLogger(ctx, logger.Logger(ctx).WithField("gameServerID", task.Server().ID()))
	}

	decision, reason := manager.checkPredecessor(ctx, task)
	switch decision {
	case predecessorWait:
		return
	case predecessorFail:
		output := []byte(reason)
		manager.notifyTaskOutput(task, output, true)
		manager.failTask(ctx, task)
	case predecessorProceed:
	}

	var err error
	if task.IsWaiting() {
		err = manager.executeTask(ctx, task)
	} else if task.IsWorking() {
		err = manager.proceedTask(ctx, task)
	}

	if err != nil {
		logger.Logger(ctx).WithError(err).Error("task execution failed")

		output := []byte(err.Error())
		manager.notifyTaskOutput(task, output, true)
		manager.failTask(ctx, task)
	}

	if task.IsComplete() {
		logger.Debug(ctx, "Task completed")

		if task.Server() != nil {
			task.Server().NoticeTaskCompleted()
		}

		manager.completed.Record(task.ID(), task.Status())
		manager.commandsInProgress.Delete(task.ID())
		manager.predecessorWaits.Delete(task.ID())
		manager.queue.Remove(task)
	}
}

type predecessorDecision int

const (
	predecessorWait predecessorDecision = iota
	predecessorProceed
	predecessorFail
)

func (manager *TaskManager) checkPredecessor(
	ctx context.Context, task *domain.GDTask,
) (predecessorDecision, string) {
	runAfterID := task.RunAfterID()
	if runAfterID <= 0 {
		return predecessorProceed, ""
	}

	if t := manager.queue.FindByID(runAfterID); t != nil {
		manager.predecessorWaits.Delete(task.ID())

		if !t.IsComplete() {
			return predecessorWait, ""
		}
		return manager.evaluatePredecessorStatus(ctx, runAfterID, t.Status())
	}

	if status, ok := manager.completed.Status(runAfterID); ok {
		manager.predecessorWaits.Delete(task.ID())

		return manager.evaluatePredecessorStatus(ctx, runAfterID, status)
	}

	return manager.waitForMissingPredecessor(ctx, task, runAfterID)
}

// waitForMissingPredecessor keeps a task waiting while its predecessor is
// neither queued nor tracked as completed, which normally means the panel has
// not delivered it yet. The wait is bounded: a predecessor that never arrives
// (or was evicted from the completion tracker) would otherwise keep the task
// in the queue forever.
func (manager *TaskManager) waitForMissingPredecessor(
	ctx context.Context, task *domain.GDTask, runAfterID int,
) (predecessorDecision, string) {
	now := time.Now()

	value, loaded := manager.predecessorWaits.LoadOrStore(task.ID(), now)
	if !loaded {
		logger.Logger(ctx).Warnf(
			"predecessor task %d not found in queue or completion tracker, waiting up to %s",
			runAfterID, manager.predecessorMissingTimeout,
		)

		return predecessorWait, ""
	}

	waitingSince, ok := value.(time.Time)
	if !ok || now.Sub(waitingSince) < manager.predecessorMissingTimeout {
		return predecessorWait, ""
	}

	manager.predecessorWaits.Delete(task.ID())

	return predecessorFail, fmt.Sprintf(
		"predecessor task %d not found after waiting %s",
		runAfterID, manager.predecessorMissingTimeout,
	)
}

func (manager *TaskManager) evaluatePredecessorStatus(
	ctx context.Context, runAfterID int, status domain.GDTaskStatus,
) (predecessorDecision, string) {
	switch status {
	case domain.GDTaskStatusSuccess:
		return predecessorProceed, ""
	case domain.GDTaskStatusError:
		return predecessorFail, fmt.Sprintf("predecessor task %d failed", runAfterID)
	case domain.GDTaskStatusCanceled:
		return predecessorFail, fmt.Sprintf("predecessor task %d was canceled", runAfterID)
	case domain.GDTaskStatusWaiting, domain.GDTaskStatusWorking:
		logger.Logger(ctx).Warnf(
			"predecessor task %d has unexpected non-terminal status %q while being evaluated",
			runAfterID, status,
		)
		return predecessorWait, ""
	default:
		logger.Logger(ctx).Warnf(
			"predecessor task %d has unknown status %q",
			runAfterID, status,
		)
		return predecessorWait, ""
	}
}

func (manager *TaskManager) executeTask(ctx context.Context, task *domain.GDTask) error {
	err := task.SetStatus(domain.GDTaskStatusWorking)
	if err != nil {
		return err
	}

	manager.notifyTaskStatus(task, "Task started")

	if task.Task() == domain.GDTaskCommandExecute {
		return manager.executeCommand(ctx, task)
	}

	return manager.executeGameCommand(ctx, task)
}

func (manager *TaskManager) executeCommand(ctx context.Context, task *domain.GDTask) error {
	cmd := newExecuteCommand(manager.config, manager.executor)

	manager.commandsInProgress.Store(task.ID(), cmd)

	logger.Debug(ctx, "Running task command")

	manager.wg.Add(1)

	go func() {
		defer manager.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logger.Logger(ctx).Errorf("panic in task command execution: %v", r)
				manager.failTask(ctx, task)
			}
		}()

		taskCtx := ctx
		if manager.config.TaskManager.TaskTimeout > 0 {
			var cancel context.CancelFunc
			taskCtx, cancel = context.WithTimeout(ctx, manager.config.TaskManager.TaskTimeout)
			defer cancel()
		}

		err := cmd.Execute(taskCtx, task.Command(), contracts.ExecutorOptions{
			WorkDir: manager.config.WorkDir(),
		})

		if err != nil {
			logger.Warn(ctx, err)
			output := []byte(err.Error())
			manager.notifyTaskOutput(task, output, true)
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

	manager.commandsInProgress.Store(task.ID(), cmdFunc)

	logger.Debug(ctx, "Running task command")

	manager.wg.Add(1)

	go func() {
		defer manager.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				logger.Logger(ctx).Errorf("panic in game command execution: %v", r)
				manager.failTask(ctx, task)
			}
		}()

		taskCtx := ctx
		if manager.config.TaskManager.TaskTimeout > 0 {
			var cancel context.CancelFunc
			taskCtx, cancel = context.WithTimeout(ctx, manager.config.TaskManager.TaskTimeout)
			defer cancel()
		}

		err := cmdFunc.Execute(taskCtx, task.Server())
		if err != nil {
			logger.Warn(ctx, err)
			output := append(cmdFunc.ReadOutput(), err.Error()...)
			manager.notifyTaskOutput(task, output, true)
			manager.failTask(ctx, task)
		}
	}()

	return nil
}

func (manager *TaskManager) proceedTask(ctx context.Context, task *domain.GDTask) error {
	c, ok := manager.commandsInProgress.Load(task.ID())
	if !ok {
		return errors.New("[gdaemon_scheduler.TaskManager] task doesn't exist in working tasks")
	}

	cmd := c.(contracts.CommandResultReader)

	output := cmd.ReadOutput()
	isFinal := cmd.IsComplete()

	// The output is sent before the terminal status, as executeCommand and
	// executeGameCommand do: the panel closes the task on the status update and
	// may drop anything arriving after it.
	manager.notifyTaskOutput(task, output, isFinal)

	if isFinal {
		manager.commandsInProgress.Delete(task.ID())

		if cmd.Result() == gameservercommands.SuccessResult {
			err := task.SetStatus(domain.GDTaskStatusSuccess)
			if err != nil {
				return err
			}
			manager.notifyTaskStatus(task, "Task completed successfully")
		} else {
			manager.failTask(ctx, task)
		}
	}

	return nil
}

func (manager *TaskManager) failTask(ctx context.Context, task *domain.GDTask) {
	err := task.SetStatus(domain.GDTaskStatusError)
	if err != nil {
		logger.Error(ctx, err)
	}

	manager.notifyTaskStatus(task, "")
}

func (manager *TaskManager) notifyTaskStatus(task *domain.GDTask, message string) {
	if manager.taskStatusSender != nil {
		manager.taskStatusSender.SendTaskStatus(task.ID(), string(task.Status()), message)
	}
}

func (manager *TaskManager) notifyTaskOutput(task *domain.GDTask, output []byte, isFinal bool) {
	if manager.taskStatusSender != nil && len(output) > 0 {
		manager.taskStatusSender.SendTaskOutput(task.ID(), output, isFinal)
	}
}

type taskQueue struct {
	tasks []*domain.GDTask
	mutex sync.RWMutex
}

func newTaskQueue() *taskQueue {
	return &taskQueue{}
}

func (q *taskQueue) Insert(tasks []*domain.GDTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.insert(tasks)
}

func (q *taskQueue) insert(tasks []*domain.GDTask) {
	for _, t := range tasks {
		existenceTask := q.findByID(t.ID())
		if existenceTask == nil {
			q.tasks = append(q.tasks, t)
		}
	}
}

func (q *taskQueue) Dequeue() *domain.GDTask {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.dequeue()
}

func (q *taskQueue) dequeue() *domain.GDTask {
	if len(q.tasks) == 0 {
		return nil
	}

	task := q.tasks[0]

	q.tasks = q.tasks[1:]

	return task
}

// Next returns first task and insert it to the end of queue.
func (q *taskQueue) Next() *domain.GDTask {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.tasks) == 0 {
		return nil
	}

	task := q.dequeue()
	q.insert([]*domain.GDTask{task})

	return task
}

func (q *taskQueue) Remove(task *domain.GDTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.tasks) == 0 {
		return
	}

	for i := range q.tasks {
		if q.tasks[i].ID() == task.ID() {
			q.tasks[i] = q.tasks[len(q.tasks)-1]
			q.tasks = q.tasks[:len(q.tasks)-1]
			break
		}
	}
}

func (q *taskQueue) FindByID(id int) *domain.GDTask {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	return q.findByID(id)
}

func (q *taskQueue) findByID(id int) *domain.GDTask {
	for _, task := range q.tasks {
		if task.ID() == id {
			return task
		}
	}

	return nil
}

func (q *taskQueue) WorkingTasks() ([]int, []*domain.GDTask) {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

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
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	return len(q.tasks)
}

type executeCommand struct {
	output   io.ReadWriter
	executor contracts.Executor
	config   *config.Config
	mu       *sync.Mutex
	result   int
	complete bool
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
