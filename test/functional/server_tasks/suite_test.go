package servertasks

import (
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	serversscheduler "github.com/gameap/daemon/internal/app/servers_scheduler"
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
	Cfg                  *config.Config

	WorkPath string
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.ServerRepository = mocks.NewServerRepository()
	suite.ServerTaskRepository = mocks.NewServerTaskRepository()
	suite.Executor = components.NewExecutor()

	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}

	suite.Scheduler = serversscheduler.NewScheduler(
		suite.Cfg,
		suite.ServerTaskRepository,
		gameservercommands.NewFactory(
			suite.Cfg,
			suite.ServerRepository,
			suite.Executor,
		),
	)
}

func (suite *Suite) SetupTest() {
	var err error

	suite.ServerRepository.Clear()
	suite.ServerTaskRepository.Clear()

	suite.WorkPath, err = ioutil.TempDir("/tmp", "gameap-daemon-test")
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
