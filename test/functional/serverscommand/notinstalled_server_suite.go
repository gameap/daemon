package serverscommand

import (
	"os"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/internal/processmanager"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/mocks"
)

type NotInstalledServerSuite struct {
	functional.GameServerSuite

	CommandFactory   *gameservercommands.ServerCommandFactory
	Cfg              *config.Config
	ServerRepository domain.ServerRepository
	Executor         contracts.Executor
	ProcessManager   contracts.ProcessManager
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
	suite.Executor = components.NewCleanExecutor()
	suite.ProcessManager = processmanager.NewSimple(suite.Cfg, suite.Executor)

	suite.CommandFactory = gameservercommands.NewFactory(suite.Cfg, suite.ServerRepository, suite.Executor, suite.ProcessManager)
}

func (suite *NotInstalledServerSuite) SetupTest() {
	var err error

	suite.WorkPath, err = os.MkdirTemp(os.TempDir(), "gameap-daemon-test")
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
