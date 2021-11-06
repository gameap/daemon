package server_repository

import "context"

func (suite *Suite) TestNotFound() {
	server, err := suite.ServerRepository.FindByID(context.Background(), 1)

	suite.Require().Nil(err)
	suite.Require().Nil(server)
}

func (suite *Suite) TestFound() {
	suite.API("/gdaemon_api/servers/1", jsonApiGetServerResponse)

	server, err := suite.ServerRepository.FindByID(context.Background(), 1)

	suite.Require().Nil(err)
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
		"default_map": "de_dust2",
	}, server.Vars())
	suite.Equal(true, server.AutoStart())
	suite.Equal("cstrike", server.Game().Code)
	suite.Equal("cstrike", server.Game().StartCode)
	suite.Equal("GoldSource", server.Game().Engine)
	suite.Equal("1", server.Game().EngineVersion)
	suite.Equal("", server.Game().RemoteRepository)
	suite.Equal("", server.Game().LocalRepository)
	suite.Equal("Counter-Strike 1.6", server.Game().Name)
	suite.Equal(4, server.GameMod().ID)
	suite.Equal("Classic (Standart)", server.GameMod().Name)
	suite.Equal("http://files.gameap.ru/cstrike-1.6/amxx.tar.xz", server.GameMod().RemoteRepository)
	suite.Equal("", server.GameMod().LocalRepository)
	suite.Equal("./hlds_run -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}", server.GameMod().DefaultStartCMDLinux)
	suite.Equal("hlds.exe -console -game cstrike +ip {ip} +port {port} +map {default_map} +maxplayers {maxplayers} +sys_ticrate {fps} +rcon_password {rcon_password}", server.GameMod().DefaultStartCMDWindows)
}
