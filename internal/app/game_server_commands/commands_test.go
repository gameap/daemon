package game_server_commands

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

func TestMakeFullCommand(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/work-path",
		Scripts: config.Scripts{
			Start: "./some-script " +
				"--dir {dir} " +
				"--id {id} " +
				"--uuid {uuid} " +
				"--uuid_short {uuid_short} " +
				"--host {host} " +
				"--ip {ip} " +
				"--port {port} " +
				"--query-port {query_port} " +
				"--rcon-port {rcon_port} " +
				"--rcon-password {rcon_password} " +
				"--game {game} " +
				"--user {user} " +
				"-- {command}",
		},
	}
	server := domain.NewServer(
		1337,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{
			StartCode: "cstrike",
		},
		domain.GameMod{
			Name: "public",
		},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		"server-dir",
		"gameap-user",
		"./start-command --default-map {default_map} --tickrate {tickrate}",
		"",
		"",
		"",
		true,
		time.Now(),
		map[string]string{
			"default_map": "de_dust2",
			"tickrate": "1000",
		},
		map[string]string{},
		time.Now(),
	)

	command := makeFullCommand(cfg, server, cfg.Scripts.Start, server.StartCommand())

	assert.Equal(t, "./some-script "+
		"--dir /work-path/server-dir "+
		"--id 1337 "+
		"--uuid 759b875e-d910-11eb-aff7-d796d7fcf7ef "+
		"--uuid_short 759b875e "+
		"--host 1.3.3.7 "+
		"--ip 1.3.3.7 "+
		"--port 1337 "+
		"--query-port 1338 "+
		"--rcon-port 1339 "+
		"--rcon-password paS$w0rD "+
		"--game cstrike "+
		"--user gameap-user "+
		"-- ./start-command --default-map de_dust2 --tickrate 1000",
		command,
	)

}
