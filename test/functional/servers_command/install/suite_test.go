package install

import (
	"os"
	"testing"

	"github.com/gameap/daemon/test/functional/servers_command"
	"github.com/otiai10/copy"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	servers_command.NotInstalledServerSuite
}

func (suite *Suite) SetupTest() {
	suite.NotInstalledServerSuite.SetupTest()

	err := os.MkdirAll(suite.WorkPath+"/repository", 0777)
	if err != nil {
		suite.T().Fatal(err)
	}
	err = copy.Copy("../../../files/local_repository", suite.WorkPath + "/repository")
	if err != nil {
		suite.T().Fatal(err)
	}
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
