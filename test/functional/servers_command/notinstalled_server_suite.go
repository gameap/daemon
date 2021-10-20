package servers_command

import (
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
		ScriptStart: "{command}",
		ScriptStop: "{command}",
	}

	suite.CommandFactory = game_server_commands.NewFactory(suite.Cfg)
}
