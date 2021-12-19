package commands

import (
	"os"
	"testing"

	"github.com/gameap/daemon/test/functional/gdtasks"
	"github.com/otiai10/copy"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	gdtasks.Suite
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()

	var err error

	suite.WorkPath, err = os.MkdirTemp(os.TempDir(), "gameap-daemon-test")
	if err != nil {
		suite.T().Fatal(err)
	}

	err = os.MkdirAll(suite.WorkPath+"/server", 0777)
	if err != nil {
		suite.T().Fatal(err)
	}

	err = copy.Copy("../../../servers/scripts", suite.WorkPath+"/server")
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Cfg.WorkPath = suite.WorkPath
	suite.Cfg.ToolsPath = suite.WorkPath + "/tools"
}

func (suite *Suite) TearDownTest() {
	suite.GDTaskRepository.Clear()

	err := os.RemoveAll(suite.WorkPath)
	if err != nil {
		suite.T().Log(err)
	}
}
