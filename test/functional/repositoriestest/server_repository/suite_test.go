package server_repository

import (
	"testing"

	"github.com/gameap/daemon/test/functional/repositoriestest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	repositoriestest.Suite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}


func (suite *Suite) SetupSuite() {
	suite.Suite.SetupSuite()
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
