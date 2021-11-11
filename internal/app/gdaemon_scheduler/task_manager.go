package gdaemon_scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var updateTimeout = 5 * time.Second

var taskServerCommandMap = map[domain.GDTaskCommand]game_server_commands.ServerCommand{
	domain.GDTaskGameServerStart:     game_server_commands.Start,
	domain.GDTaskGameServerPause:     game_server_commands.Pause,
	domain.GDTaskGameServerStop:      game_server_commands.Stop,
	domain.GDTaskGameServerKill:      game_server_commands.Kill,
	domain.GDTaskGameServerRestart:   game_server_commands.Restart,
	domain.GDTaskGameServerInstall:   game_server_commands.Install,
	domain.GDTaskGameServerReinstall: game_server_commands.Reinstall,
	domain.GDTaskGameServerUpdate:    game_server_commands.Update,
	domain.GDTaskGameServerDelete:    game_server_commands.Delete,
}

type TaskManager struct {
	config               *config.Config
	repository           domain.GDTaskRepository
	serverCommandFactory *game_server_commands.ServerCommandFactory

	// Runtime
	lastUpdated          time.Time
	commandsInProgress   sync.Map // map[domain.GDTask]interfaces.Command
	queue                taskQueue
	cache                interfaces.Cache

}

func NewTaskManager(
	repository domain.GDTaskRepository,
	cache interfaces.Cache,
	serverCommandFactory *game_server_commands.ServerCommandFactory,
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
	//tasksToRepeat, err := manager.repository.FindByStatus(ctx, domain.GDTaskStatusWorking)
	//if err != nil {
	//	return err
	//}
	//manager.queue.Insert(tasksToRepeat)

	err := manager.updateTasksIfNeeded(ctx)

	if err != nil {
		log.Error(err)
	}

	for {
		select {
		case <-(ctx).Done():
			return nil
		default:
			manager.runNext(ctx)

			err = manager.updateTasksIfNeeded(ctx)
			if err != nil {
				log.Error(err)
			}

			time.Sleep(5 * time.Second)
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

	var err error
	if task.IsWaiting() {
		err = manager.executeTask(ctx, task)
	} else if task.IsWorking() {
		err = manager.proceedTask(ctx, task)
	}

	if err != nil {
		log.Error(errors.WithMessage(err, "task execution failed"))

		manager.appendTaskOutput(ctx, task, []byte(err.Error()))
		manager.failTask(ctx, task)
	}

	if task.IsComplete() {
		manager.queue.Remove(task)

		err = manager.repository.Save(ctx, task)
		if err != nil {
			err = errors.WithMessage(err, "[gdaemon_scheduler.TaskManager] failed to save task")
			log.Error(err)
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
		log.Error(err)
	}

	cmd, gameServerCmdExist := taskServerCommandMap[task.Task()]

	if !gameServerCmdExist {
		return ErrInvalidTaskError
	}

	cmdFunc := manager.serverCommandFactory.LoadServerCommandFunc(cmd)

	manager.commandsInProgress.Store(*task, cmdFunc)

	go func() {
		err = cmdFunc.Execute(ctx, task.Server())
		if err != nil {
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
		if cmd.Result() == game_server_commands.SuccessResult {
			err := task.SetStatus(domain.GDTaskStatusSuccess)
			if err != nil {
				return err
			}
		} else {
			manager.failTask(ctx, task)
		}
	}

	manager.appendTaskOutput(ctx, task, cmd.ReadOutput())

	return nil
}

func (manager *TaskManager) failTask(_ context.Context, task *domain.GDTask) {
	err := task.SetStatus(domain.GDTaskStatusError)
	if err != nil {
		log.Error(err)
	}
}

func (manager *TaskManager) appendTaskOutput(ctx context.Context, task *domain.GDTask, output []byte) {
	if len(output) == 0 {
		return
	}

	err := manager.repository.AppendOutput(ctx, task, output)
	if err != nil {
		log.Error(err)
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
