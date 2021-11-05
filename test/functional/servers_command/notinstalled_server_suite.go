package servers_command

import (
	"io/ioutil"
	"os"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/test/functional"
)

type NotInstalledServerSuite struct {
	functional.GameServerSuite

	CommandFactory *game_server_commands.ServerCommandFactory
	Cfg              *config.Config
	WorkPath         string
}

func (suite *NotInstalledServerSuite) SetupSuite() {
	suite.Cfg = &config.Config{
		Scripts: config.Scripts{
			Start: "{command}",
			Stop: "{command}",
		},
	}

	suite.CommandFactory = game_server_commands.NewFactory(suite.Cfg)
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
	os.RemoveAll(suite.WorkPath)
}
