package serverscommand

import (
	"os"

	"github.com/gameap/daemon/internal/app/fsutil"
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
	err = fsutil.Copy("../../../servers/scripts", suite.WorkPath+"/server", fsutil.CopyOptions{})
	if err != nil {
		suite.T().Fatal()
	}

	suite.Cfg.WorkPath = suite.WorkPath
}

func (suite *InstalledServerSuite) TearDownTest() {
	suite.NotInstalledServerSuite.TearDownTest()
}

func (suite *InstalledServerSuite) GivenServerIsDown() {
	suite.Cfg.Scripts.Status = CommandFailScript + " status"
}

func (suite *InstalledServerSuite) GivenServerIsActive() {
	suite.Cfg.Scripts.Status = CommandScript + " status"
}
