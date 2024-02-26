package servertasks

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
	"github.com/gameap/daemon/internal/processmanager"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
	"github.com/otiai10/copy"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	functional.GameServerSuite

	Scheduler            *serversscheduler.Scheduler
	ServerTaskRepository *mocks.ServerTaskRepository
	ServerRepository     *mocks.ServerRepository
	Executor             contracts.Executor
	ProcessManager       contracts.ProcessManager
	Cfg                  *config.Config

	WorkPath string
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}

	suite.ServerRepository = mocks.NewServerRepository()
	suite.ServerTaskRepository = mocks.NewServerTaskRepository()
	suite.Executor = components.NewExecutor()
	suite.ProcessManager = processmanager.NewSimple(suite.Cfg, suite.Executor, suite.Executor)
}

func (suite *Suite) SetupTest() {
	var err error

	suite.ServerRepository.Clear()
	suite.ServerTaskRepository.Clear()

	suite.Scheduler = serversscheduler.NewScheduler(
		suite.Cfg,
		suite.ServerTaskRepository,
		gameservercommands.NewFactory(
			suite.Cfg,
			suite.ServerRepository,
			suite.Executor,
			suite.ProcessManager,
		),
	)

	suite.WorkPath, err = os.MkdirTemp(os.TempDir(), "gameap-daemon-test")
	if err != nil {
		suite.T().Fatal(err)
	}

	err = os.MkdirAll(suite.WorkPath+"/server", 0777)
	if err != nil {
		suite.T().Fatal(err)
	}

	err = copy.Copy("../../servers/scripts", suite.WorkPath+"/server")
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Cfg.WorkPath = suite.WorkPath
}

func (suite *Suite) GivenTask(id int, executeDate time.Time, repeatPeriod time.Duration) *domain.ServerTask {
	server := suite.GivenServerWithStartCommand("./make_file_with_contents.sh")
	task := domain.NewServerTask(
		id,
		domain.ServerTaskStart,
		server,
		0,
		repeatPeriod,
		0,
		executeDate,
	)

	suite.ServerRepository.Set([]*domain.Server{server})
	suite.ServerTaskRepository.Set([]*domain.ServerTask{task})

	return task
}

func (suite *Suite) TearDownTest() {
	err := os.RemoveAll(suite.WorkPath)
	if err != nil {
		suite.T().Log(err)
	}
}

func (suite *Suite) RunServerSchedulerWithTimeout(duration time.Duration) {
	suite.T().Helper()

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	err := suite.Scheduler.Run(ctx)

	suite.Require().NoError(err)
}

func (suite *Suite) RunServerSchedulerUntilTaskCounterIncreased(task *domain.ServerTask) {
	initTaskCounter := task.Counter()
	startedAt := time.Now()

	ctx, cancel := context.WithCancel(context.Background())

	go func(t *testing.T) {
		t.Helper()

		err := suite.Scheduler.Run(ctx)
		if err != nil {
			t.Log(err)
		}
	}(suite.T())

	for {
		if time.Since(startedAt) >= 10*time.Second {
			cancel()
			break
		}

		if task.Counter() > initTaskCounter {
			cancel()
			break
		}
	}

	time.Sleep(1 * time.Second)
}
