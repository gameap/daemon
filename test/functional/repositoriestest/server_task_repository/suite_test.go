package servertaskrepository

import (
	"context"
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

	serverTaskRepository, err := suite.Container.ServerTaskRepository(context.TODO())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.ServerTaskRepository = serverTaskRepository.(*repositories.ServerTaskRepository)
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
