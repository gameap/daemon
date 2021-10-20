package servers_command

import (
	"io/ioutil"
	"os"

	"github.com/otiai10/copy"
)

type InstalledServerSuite struct {
	NotInstalledServerSuite
}

//func (suite *InstalledServerSuite) SetupSuite() {
//	suite.NotInstalledServerSuite.SetupSuite()
//}

func (suite *InstalledServerSuite) SetupTest() {
	var err error

	suite.WorkPath, err = ioutil.TempDir("/tmp", "gameap-daemon-test")
	if err != nil {
		suite.T().Fatal(err)
	}
	os.MkdirAll(suite.WorkPath + "/server", 0777)
	copy.Copy("../../../servers/scripts", suite.WorkPath + "/server")

	suite.Cfg.WorkPath = suite.WorkPath
}

func (suite *InstalledServerSuite) TearDownTest() {
	os.RemoveAll(suite.WorkPath)
}

func (suite *InstalledServerSuite) GivenServerIsDown() {
	suite.Cfg.ScriptStatus = "./command_fail.sh status"
}

func (suite *InstalledServerSuite) GivenServerIsActive() {
	suite.Cfg.ScriptStatus = "./command.sh status"
}
