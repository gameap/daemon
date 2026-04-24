package serverrepository

import (
	"context"
	"net/http"

	"github.com/gameap/daemon/test/functional/repositoriestest"
)

func (suite *Suite) TestNotFound() {
	server, err := suite.ServerRepository.FindByID(context.Background(), 99999)

	suite.Require().Nil(err)
	suite.Require().Nil(server)
}

func (suite *Suite) TestSuccess() {
	suite.GivenAPIResponse("/gdaemon_api/servers/1", http.StatusOK, repositoriestest.JSONApiGetServerResponseBody)

	server, err := suite.ServerRepository.FindByID(context.Background(), 1)

	suite.Require().Nil(err)
	suite.Require().NotNil(server)
	suite.Equal(1, server.ID())
	suite.Equal("94cdfde4-15a4-40b9-8043-260e6a0b5b67", server.UUID())
	suite.Equal("94cdfde4", server.UUIDShort())
	suite.Equal("1", server.Setting("autostart_current"))
	suite.Equal("servers/94cdfde4-15a4-40b9-8043-260e6a0b5b67", server.Dir())
	suite.Equal("172.24.0.5", server.IP())
	suite.Equal(27015, server.ConnectPort())
	suite.Equal(27015, server.QueryPort())
	suite.Equal(27015, server.RCONPort())
	suite.Equal("57jPyiVYTO", server.RCONPassword())
	suite.Equal("gameap", server.User())
	suite.Equal("./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}", server.StartCommand())
	suite.Equal(map[string]string{
		"default_map":       "de_dust2",
		"fps":               "500",
		"maxplayers":        "32",
		"autostart_current": "1",
	}, server.Vars())
	suite.Equal(true, server.AutoStart())
	suite.Equal("cstrike", server.Game().Code)
	suite.Equal("cstrike", server.Game().StartCode)
	suite.Equal("GoldSource", server.Game().Engine)
	suite.Equal("1", server.Game().EngineVersion)
	suite.Equal("http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz", server.Game().RemoteRepository)
	suite.Equal("/srv/gameap/repository/hlcs_base.tar.xz", server.Game().LocalRepository)
	suite.Equal("Counter-Strike 1.6", server.Game().Name)
	suite.Equal(4, server.GameMod().ID)
	suite.Equal("Classic (Standart)", server.GameMod().Name)
	suite.Equal("http://files.gameap.ru/cstrike-1.6/amxx.tar.xz", server.GameMod().RemoteRepository)
	suite.Equal("/srv/gameap/repository/cstrike-1.6/amxx.tar.xz", server.GameMod().LocalRepository)
	suite.Equal("./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}", server.GameMod().DefaultStartCMDLinux)
	suite.Equal("hlds.exe -console -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}", server.GameMod().DefaultStartCMDWindows)
	suite.Equal("custom_value", server.Game().Metadata["custom_key"])
	suite.Equal("mod_value", server.GameMod().Metadata["mod_key"])
}

func (suite *Suite) TestWhenTokenIsInvalid_ExpectSuccess() {
	suite.GivenAPIResponse("/gdaemon_api/servers/1", http.StatusUnauthorized, nil)
	suite.GivenAPIResponse("/gdaemon_api/get_token", http.StatusOK, repositoriestest.JSONApiGetTokenResponseBody)
	suite.GivenAPIResponse("/gdaemon_api/servers/1", http.StatusOK, repositoriestest.JSONApiGetServerResponseBody)

	server, err := suite.ServerRepository.FindByID(context.Background(), 1)

	suite.Require().Nil(err)
	suite.Require().NotNil(server)
	suite.Equal(1, server.ID())
}

func (suite *Suite) TestFactorioServerParsing() {
	suite.GivenAPIResponse("/gdaemon_api/servers/2", http.StatusOK, repositoriestest.JSONApiGetServerFactorioResponseBody)

	server, err := suite.ServerRepository.FindByID(context.Background(), 2)

	suite.Require().Nil(err)
	suite.Require().NotNil(server)

	// Basic server info
	suite.Equal(2, server.ID())
	suite.Equal("9c3dea74-b4d6-4e2f-9f4e-6c97b6e3f9a2", server.UUID())
	suite.Equal("9c3dea74", server.UUIDShort())
	suite.Equal("servers/9c3dea74-b4d6-4e2f-9f4e-6c97b6e3f9a2", server.Dir())
	suite.Equal("192.168.1.100", server.IP())
	suite.Equal(27023, server.ConnectPort())
	suite.Equal(27023, server.QueryPort())
	suite.Equal(27023, server.RCONPort())
	suite.Equal("factoriorcon", server.RCONPassword())
	suite.Equal("gameap", server.User())

	// Server settings parsed correctly from array format
	suite.Equal("false", server.Setting("update_before_start"))
	suite.Equal("Description", server.Setting("SERVER_DESC"))
	suite.Equal("unnamed", server.Setting("SERVER_USERNAME"))
	suite.Equal("1.1.100", server.Setting("FACTORIO_VERSION"))
	suite.Equal("20", server.Setting("MAX_SLOTS"))
	suite.Equal("gamesave", server.Setting("SAVE_NAME"))

	vars := server.Vars()
	suite.Equal("1.1.100", vars["FACTORIO_VERSION"])
	suite.Equal("20", vars["MAX_SLOTS"])
	suite.Equal("gamesave", vars["SAVE_NAME"])
	suite.Equal("Description", vars["SERVER_DESC"])

	// Game info
	suite.Equal("factorio", server.Game().Code)
	suite.Equal("factorio", server.Game().StartCode)
	suite.Equal("Factorio", server.Game().Engine)
	suite.Equal("Factorio", server.Game().Name)

	// GameMod info
	suite.Equal(10, server.GameMod().ID)
	suite.Equal("Vanilla", server.GameMod().Name)
	suite.Len(server.GameMod().Vars, 4)
}

func (suite *Suite) TestEnvironmentVars() {
	suite.GivenAPIResponse("/gdaemon_api/servers/2", http.StatusOK, repositoriestest.JSONApiGetServerFactorioResponseBody)

	server, err := suite.ServerRepository.FindByID(context.Background(), 2)

	suite.Require().Nil(err)
	suite.Require().NotNil(server)

	envVars := server.EnvironmentVars()

	// Port values are always set
	suite.Equal("27023", envVars["SERVER_PORT"])
	suite.Equal("27023", envVars["PORT"])
	suite.Equal("27023", envVars["QUERY_PORT"])
	suite.Equal("27023", envVars["RCON_PORT"])

	// Settings override gameMod.Vars defaults
	// FACTORIO_VERSION: default "latest" -> setting "1.1.100"
	suite.Equal("1.1.100", envVars["FACTORIO_VERSION"])
	// MAX_SLOTS: default "10" -> setting "20"
	suite.Equal("20", envVars["MAX_SLOTS"])
	// SAVE_NAME: default "world" -> setting "gamesave"
	suite.Equal("gamesave", envVars["SAVE_NAME"])
	// SERVER_DESC: default "A Factorio Server" -> setting "Description"
	suite.Equal("Description", envVars["SERVER_DESC"])

	// Additional settings that weren't in gameMod.Vars (keys are normalized)
	suite.Equal("false", envVars["UPDATE_BEFORE_START"])
	suite.Equal("unnamed", envVars["SERVER_USERNAME"])
}

func (suite *Suite) TestEnvironmentVarsWithServerVars() {
	suite.GivenAPIResponse("/gdaemon_api/servers/1", http.StatusOK, repositoriestest.JSONApiGetServerResponseBody)

	server, err := suite.ServerRepository.FindByID(context.Background(), 1)

	suite.Require().Nil(err)
	suite.Require().NotNil(server)

	envVars := server.EnvironmentVars()

	// Port values are always set
	suite.Equal("27015", envVars["SERVER_PORT"])
	suite.Equal("27015", envVars["PORT"])
	suite.Equal("27015", envVars["QUERY_PORT"])
	suite.Equal("27015", envVars["RCON_PORT"])

	// server.vars override gameMod.Vars defaults (keys are normalized to uppercase)
	// default_map: gameMod default "de_dust2" -> server var "de_dust2" (same in this case)
	suite.Equal("de_dust2", envVars["DEFAULT_MAP"])
	// fps: gameMod default "500" (no override)
	suite.Equal("500", envVars["FPS"])
	// maxplayers: gameMod default "32" (no override)
	suite.Equal("32", envVars["MAXPLAYERS"])

	// Settings from server.settings (keys are normalized)
	suite.Equal("1", envVars["AUTOSTART_CURRENT"])
}
