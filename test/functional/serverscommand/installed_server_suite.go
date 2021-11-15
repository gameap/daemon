package serverscommand

import (
	"os"

	"github.com/otiai10/copy"
)

type InstalledServerSuite struct {
	NotInstalledServerSuite
}

func (suite *InstalledServerSuite) SetupTest() {
	suite.NotInstalledServerSuite.SetupTest()

	err := os.MkdirAll(suite.WorkPath+"/server", 0777)
	if err != nil {
		suite.T().Fatal()
	}
	err = copy.Copy("../../../servers/scripts", suite.WorkPath+"/server")
	if err != nil {
		suite.T().Fatal()
	}

	suite.Cfg.WorkPath = suite.WorkPath
}

func (suite *InstalledServerSuite) TearDownTest() {
	suite.NotInstalledServerSuite.TearDownTest()
}

func (suite *InstalledServerSuite) GivenServerIsDown() {
	suite.Cfg.Scripts.Status = "./command_fail.sh status"
}

func (suite *InstalledServerSuite) GivenServerIsActive() {
	suite.Cfg.Scripts.Status = "./command.sh status"
}
