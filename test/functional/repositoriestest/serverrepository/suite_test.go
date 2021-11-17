package serverrepository

import (
	"context"
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

	serverRepository, err := suite.Container.ServerRepository(context.TODO())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.ServerRepository = serverRepository.(*repositories.ServerRepository)
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
