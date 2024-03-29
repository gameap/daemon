package gdtasks

import (
	"context"
	"io"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	gdaemonscheduler "github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/gameap/daemon/internal/processmanager"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
)

const taskManagerTimeout = 300 * time.Second

type Suite struct {
	functional.GameServerSuite

	TaskManager      *gdaemonscheduler.TaskManager
	GDTaskRepository *mocks.GDTaskRepository
	ServerRepository *mocks.ServerRepository
	Executor         contracts.Executor
	ProcessManager   contracts.ProcessManager
	Cache            contracts.Cache
	Cfg              *config.Config

	WorkPath string
}

func (suite *Suite) SetupTest() {
	suite.TaskManager = gdaemonscheduler.NewTaskManager(
		suite.GDTaskRepository,
		suite.Cache,
		gameservercommands.NewFactory(
			suite.Cfg,
			suite.ServerRepository,
			suite.Executor,
			suite.ProcessManager,
		),
		suite.Executor,
		suite.Cfg,
	)
}

func (suite *Suite) SetupSuite() {
	var err error

	suite.GDTaskRepository = mocks.NewGDTaskRepository()
	suite.ServerRepository = mocks.NewServerRepository()

	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}

	executor := components.NewDefaultExtendableExecutor(components.NewCleanExecutor())
	suite.ProcessManager = processmanager.NewSimple(suite.Cfg, executor, executor)

	executor.RegisterHandler("get-tool", customhandlers.NewGetTool(suite.Cfg).Handle)
	suite.Executor = executor

	suite.Cache, err = services.NewLocalCache(suite.Cfg)
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) RunTaskManager(timeout time.Duration) {
	suite.T().Helper()

	ctx, timeoutCancel := context.WithTimeout(context.Background(), timeout)
	defer timeoutCancel()

	err := suite.TaskManager.Run(ctx)
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) RunTaskManagerUntilTasksCompleted(tasks []*domain.GDTask) {
	suite.T().Helper()
	startedAt := time.Now()

	ctx, cancel := context.WithCancel(context.Background())

	go func(t *testing.T) {
		t.Helper()

		err := suite.TaskManager.Run(ctx)
		if err != nil {
			t.Log(err)
		}
	}(suite.T())

	for {
		if time.Since(startedAt) >= taskManagerTimeout {
			cancel()
			break
		}

		if suite.isAllTasksCompleted(tasks) {
			cancel()
			break
		}
	}

	time.Sleep(1 * time.Second)
}

func (suite *Suite) isAllTasksCompleted(tasks []*domain.GDTask) bool {
	counter := 0
	for i := range tasks {
		if tasks[i].IsComplete() {
			counter++
		}
	}

	return counter >= len(tasks)
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
	if task.Server() != nil {
		suite.Assert().Equal(task.Server().ID(), actualTask.Server().ID())
	} else {
		suite.Assert().Nil(actualTask.Server())
	}
}

func (suite *Suite) GivenGDTaskWithCommand(cmd string) *domain.GDTask {
	minID := 100
	maxID := 1000000000
	task := domain.NewGDTask(
		rand.Intn(maxID-minID)+minID,
		0,
		nil,
		domain.GDTaskCommandExecute,
		cmd,
		domain.GDTaskStatusWaiting,
	)

	suite.GDTaskRepository.Set([]*domain.GDTask{task})

	return task
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

	rand.New(rand.NewSource(time.Now().UnixNano()))
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

	buf, err := io.ReadAll(fd)

	suite.Assert().Equal(expectedContents, buf)
}
