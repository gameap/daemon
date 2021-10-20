package gdtasks

import (
	"context"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/gameap/daemon/internal/app"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/gdaemon_scheduler"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
)

type Suite struct {
	functional.GameServerSuite

	TaskManager      *gdaemon_scheduler.TaskManager
	GDTaskRepository *mocks.GDTaskRepository
	Cache            interfaces.Cache
	Cfg              *config.Config

	WorkPath         string
}

func (suite *Suite) SetupSuite() {
	var err error

	suite.GDTaskRepository = mocks.NewGDTaskRepository()

	suite.Cfg = &config.Config{
		ScriptStart: "{command}",
		ScriptStop: "{command}",
	}

	suite.Cache, err = app.NewLocalCache(suite.Cfg)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.TaskManager = gdaemon_scheduler.NewTaskManager(
		suite.GDTaskRepository,
		suite.Cache,
		game_server_commands.NewFactory(suite.Cfg),
		suite.Cfg,
	)
}

func (suite *Suite) RunTaskManagerWithTimeout(duration time.Duration) {
	suite.T().Helper()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	err := suite.TaskManager.Run(ctx)

	suite.Require().NoError(err)
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
			1,
			0,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusWaiting,
		),
		domain.NewGDTask(
			2,
			1,
			server,
			domain.GDTaskGameServerStop,
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
			5,
			3,
			server,
			domain.GDTaskGameServerStart,
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
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(tasks), func(i, j int) { tasks[i], tasks[j] = tasks[j], tasks[i]})

	suite.GDTaskRepository.Set(tasks)

	return tasks
}

func (suite *Suite) GivenServerWithStartCommand(startCommand string) *domain.Server {
	return suite.GivenServerWithStartAndStopCommand(startCommand, "")
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
