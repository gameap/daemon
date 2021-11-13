package servertaskrepository

import (
	"testing"

	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gameap/daemon/test/functional/repositoriestest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	repositoriestest.Suite

	ServerTaskRepository *repositories.ServerTaskRepository
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.Suite.SetupSuite()

	suite.ServerTaskRepository = suite.Container.Get("serverTaskRepository").(*repositories.ServerTaskRepository)
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
