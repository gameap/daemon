package serverrepository

import (
	"testing"

	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gameap/daemon/test/functional/repositoriestest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	repositoriestest.Suite

	ServerRepository *repositories.ServerRepository
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.Suite.SetupSuite()

	suite.ServerRepository = suite.Container.Get("serverRepository").(*repositories.ServerRepository)
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
