package install

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
)

func (suite *Suite) TestInstall_NoRules() {
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)
	server.SetInstallationStatus(domain.ServerNotInstalled)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Install, server)

	err := cmd.Execute(context.Background(), server)

	suite.ErrorIs(err, gameservercommands.ErrDefinedNoGameInstallationRulesError)
}

func (suite *Suite) TestInstall_InstallFromRemoteRepository_GameInstalled() {
	server := suite.GivenServerForGameAndMod(
		domain.Game{
			StartCode:        "cstrike",
			RemoteRepository: "https://files.gameap.ru/test/test.tar.xz",
		},
		domain.GameMod{
			Name: "public",
		},
	)
	server.SetInstallationStatus(domain.ServerNotInstalled)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Install, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NoError(err)
	suite.FileExists(suite.WorkPath + "/server/run.sh")
	suite.NoFileExists(suite.WorkPath + "/server/.gamemodinstalled")
}

func (suite *Suite) TestInstall_InstallFromRemoteRepository_GameAndModInstalled() {
	server := suite.GivenServerForGameAndMod(
		domain.Game{
			StartCode:        "cstrike",
			RemoteRepository: "https://files.gameap.ru/test/test.tar.xz",
		},
		domain.GameMod{
			Name:             "public",
			RemoteRepository: "https://files.gameap.ru/mod-game.tar.gz",
		},
	)
	server.SetInstallationStatus(domain.ServerNotInstalled)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Install, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NoError(err)
	suite.FileExists(suite.WorkPath + "/server/run.sh")
	suite.FileExists(suite.WorkPath + "/server/.gamemodinstalled")
}

func (suite *Suite) TestInstall_InstallFromLocalRepository_GameAndModInstalledFromLocalRepository() {
	server := suite.GivenServerForGameAndMod(
		domain.Game{
			StartCode:        "cstrike",
			RemoteRepository: "https://files.gameap.ru/test/test.tar.xz",
			LocalRepository:  suite.WorkPath + "/repository/game.tar.gz",
		},
		domain.GameMod{
			Name:             "public",
			RemoteRepository: "https://files.gameap.ru/mod-game.tar.gz",
			LocalRepository:  suite.WorkPath + "/repository/game_mod.tar.gz",
		},
	)
	server.SetInstallationStatus(domain.ServerNotInstalled)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Install, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NoError(err)
	suite.FileExists(suite.WorkPath + "/server/game_file_from_tar_gz")
	suite.FileExists(suite.WorkPath + "/server/game_mod_file_from_tar_gz")
	suite.NoFileExists(suite.WorkPath + "/server/run.sh")
	suite.NoFileExists(suite.WorkPath + "/server/.gamemodinstalled")
}
