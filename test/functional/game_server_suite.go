package functional

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/suite"
)

type GameServerSuite struct {
	suite.Suite
}

func (suite *GameServerSuite) GivenServerWithStartAndStopCommand(startCommand string, stopCommand string) *domain.Server {
	suite.T().Helper()

	return domain.NewServer(
		1337,
		1,
		1,
		domain.ServerInstalled,
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
		"server",
		"gameap-user",
		startCommand,
		stopCommand,
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
}
