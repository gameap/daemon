package servers_command

import (
	"os"

	"github.com/otiai10/copy"
)

type InstalledServerSuite struct {
	NotInstalledServerSuite
}

func (suite *InstalledServerSuite) SetupTest() {
	suite.NotInstalledServerSuite.SetupTest()

	os.MkdirAll(suite.WorkPath + "/server", 0777)
	copy.Copy("../../../servers/scripts", suite.WorkPath + "/server")

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
