package serverscommand

import (
	"io/ioutil"
	"os"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
)

type NotInstalledServerSuite struct {
	functional.GameServerSuite

	CommandFactory   *gameservercommands.ServerCommandFactory
	Cfg              *config.Config
	ServerRepository domain.ServerRepository
	Executor         interfaces.Executor
	WorkPath         string
}

func (suite *NotInstalledServerSuite) SetupSuite() {
	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop:  "{command}",
		},
	}

	suite.ServerRepository = mocks.NewServerRepository()
	suite.Executor = components.NewClearExecutor()

	suite.CommandFactory = gameservercommands.NewFactory(suite.Cfg, suite.ServerRepository, suite.Executor)
}

func (suite *NotInstalledServerSuite) SetupTest() {
	var err error

	suite.WorkPath, err = ioutil.TempDir("/tmp", "gameap-daemon-test")
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Cfg.WorkPath = suite.WorkPath
}

func (suite *NotInstalledServerSuite) TearDownTest() {
	err := os.RemoveAll(suite.WorkPath)
	if err != nil {
		suite.T().Log(err)
	}
}
