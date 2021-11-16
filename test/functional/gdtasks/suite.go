package gdtasks

import (
	"context"
	"io/ioutil"
	"math/rand"
	"os"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/gameap/daemon/internal/app"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
)

type Suite struct {
	functional.GameServerSuite

	TaskManager      *gdaemonscheduler.TaskManager
	GDTaskRepository *mocks.GDTaskRepository
	ServerRepository *mocks.ServerRepository
	Executor         interfaces.Executor
	Cache            interfaces.Cache
	Cfg              *config.Config

	WorkPath string
}

func (suite *Suite) SetupSuite() {
	var err error

	suite.GDTaskRepository = mocks.NewGDTaskRepository()
	suite.ServerRepository = mocks.NewServerRepository()

	suite.Executor = components.NewExecutor()

	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}

	suite.Cache, err = app.NewLocalCache(suite.Cfg)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.TaskManager = gdaemonscheduler.NewTaskManager(
		suite.GDTaskRepository,
		suite.Cache,
		gameservercommands.NewFactory(
			suite.Cfg,
			suite.ServerRepository,
			suite.Executor,
		),
		suite.Cfg,
	)
}

func (suite *Suite) RunTaskManager() {
	suite.T().Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3 * time.Minute)
	defer cancel()

	go func() {
		err := suite.TaskManager.Run(ctx)
		if err != nil {
			panic(err)
		}
	}()

	suite.waitForTaskManagerCompleteAllTasks()
}

func (suite *Suite) waitForTaskManagerCompleteAllTasks() {
	for {
		time.Sleep(1 * time.Second)

		rv := reflect.ValueOf(suite.TaskManager)
		val := reflect.Indirect(rv)

		queue := val.FieldByName("queue")
		tasks := queue.FieldByName("tasks")
		ptrToTasks := unsafe.Pointer(tasks.UnsafeAddr())
		realTasks := (*[]*domain.GDTask)(ptrToTasks)

		commandsInProgressRef := val.FieldByName("commandsInProgress")
		ptrToCommandsInProgress := unsafe.Pointer(commandsInProgressRef.UnsafeAddr())
		realCommandsInProgress := (*sync.Map)(ptrToCommandsInProgress)

		commandsCount := 0
		realCommandsInProgress.Range(func(key, value interface{}) bool {
			commandsCount++
			return true
		})

		if len(*realTasks) == 0 && commandsCount == 0 {
			break
		}
	}
}

func (suite *Suite) AssertGDTaskExist(task *domain.GDTask) {
	suite.T().Helper()

	actualTask, err := suite.GDTaskRepository.FindByID(context.Background(), task.ID())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Require().NotNil(actualTask)
	suite.Assert().Equal(task.Status(), actualTask.Status())
	suite.Assert().Equal(task.RunAfterID(), actualTask.RunAfterID())
	suite.Assert().Equal(task.Task(), actualTask.Task())
	suite.Assert().Equal(task.Server().ID(), actualTask.Server().ID())
}

func (suite *Suite) GivenGDTaskWithIDForServer(id int, server *domain.Server) *domain.GDTask {
	task := domain.NewGDTask(
		id,
		0,
		server,
		domain.GDTaskGameServerStart,
		"",
		domain.GDTaskStatusWaiting,
	)

	suite.GDTaskRepository.Set([]*domain.GDTask{task})

	return task
}

func (suite *Suite) GivenSequenceGDTaskForServer(server *domain.Server) []*domain.GDTask {
	suite.T().Helper()

	tasks := []*domain.GDTask{
		domain.NewGDTask(
			2,
			1,
			server,
			domain.GDTaskGameServerStop,
			"",
			domain.GDTaskStatusWaiting,
		),
		domain.NewGDTask(
			7,
			5,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusWaiting,
		),
		domain.NewGDTask(
			3,
			2,
			server,
			domain.GDTaskGameServerStop,
			"",
			domain.GDTaskStatusWaiting,
		),
		domain.NewGDTask(
			1,
			0,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusWaiting,
		),
		domain.NewGDTask(
			5,
			3,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusWaiting,
		),
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(tasks), func(i, j int) { tasks[i], tasks[j] = tasks[j], tasks[i] })

	suite.GDTaskRepository.Set(tasks)

	return tasks
}

func (suite *Suite) AssertFileContents(file string, expectedContents []byte) {
	suite.T().Helper()

	suite.Require().FileExists(file)

	fd, err := os.Open(file)
	if err != nil {
		suite.T().Fatal(err)
	}
	defer func() {
		if err = fd.Close(); err != nil {
			suite.T().Fatal(err)
		}
	}()

	buf, err := ioutil.ReadAll(fd)

	suite.Assert().Equal(expectedContents, buf)
}
