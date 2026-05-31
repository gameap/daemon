package install

import (
	"os"
	"testing"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/test/functional/serverscommand"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	serverscommand.NotInstalledServerSuite
}

func (suite *Suite) SetupTest() {
	suite.NotInstalledServerSuite.SetupTest()

	err := os.MkdirAll(suite.WorkPath+"/repository", 0777)
	if err != nil {
		suite.T().Fatal(err)
	}
	err = fsutil.Copy("../../../files/local_repository", suite.WorkPath+"/repository", fsutil.CopyOptions{})
	if err != nil {
		suite.T().Fatal(err)
	}
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
