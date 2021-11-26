package commands

import (
	"io/ioutil"
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
	var err error

	suite.GDTaskRepository.Clear()

	suite.WorkPath, err = ioutil.TempDir("/tmp", "gameap-daemon-test")
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
}

func (suite *Suite) TearDownTest() {
	err := os.RemoveAll(suite.WorkPath)
	if err != nil {
		suite.T().Log(err)
	}
}
