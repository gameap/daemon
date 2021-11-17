package gdtaskrepository

import (
	"context"
	"testing"

	"github.com/gameap/daemon/internal/app/repositories"
	"github.com/gameap/daemon/test/functional/repositoriestest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	repositoriestest.Suite

	GDTaskRepository *repositories.GDTaskRepository
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.Suite.SetupSuite()

	gdTaskRepository, err := suite.Container.GdTaskRepository(context.TODO())
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.GDTaskRepository = gdTaskRepository.(*repositories.GDTaskRepository)
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()
}
