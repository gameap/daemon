package files

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/test/functional/servertest"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	servertest.Suite

	tempFileDestination string
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()

	tempDirDestination := os.TempDir() + "/files_test_destination_" + strconv.Itoa(int(time.Now().UnixNano()))
	suite.tempFileDestination = tempDirDestination + "/file"
}

func (suite *Suite) TearDownSuite() {
	suite.Suite.TearDownSuite()
}

func (suite *Suite) Authenticate() {
	suite.T().Helper()
	suite.Auth(server.ModeFiles)
}

func (suite *Suite) readMessageFromClient() []interface{} {
	suite.T().Helper()

	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var msg []interface{}
	err := decode.Unmarshal(buf, &msg)
	if err != nil {
		suite.T().Fatal(err)
	}

	return msg
}

func (suite *Suite) assertUploadedFileContents(expected []byte) {
	contents, err := os.ReadFile(suite.tempFileDestination)
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.Equal(expected, contents)
}

func (suite *Suite) assertFirstAndLastFileBytes(first []byte, last []byte) {
	contents, err := os.ReadFile(suite.tempFileDestination)
	if err != nil {
		suite.T().Fatal(err)
	}

	firstLen := len(first)
	lastLen := len(last)

	suite.Equal(first, contents[:firstLen])
	suite.Equal(last, contents[len(contents)-lastLen:])
}

func (suite *Suite) assertUploadedFileSize(expected int) {
	suite.T().Helper()

	suite.Require().FileExists(suite.tempFileDestination)
	stat, err := os.Stat(suite.tempFileDestination)
	if err != nil {
		suite.T().Fatal(err)
	}

	suite.Equal(int64(expected), stat.Size())
}

func (suite *Suite) givenUploadMessage(size int) []interface{} {
	return []interface{}{
		files.FileSend,
		files.GetFileFromClient,
		suite.tempFileDestination,
		uint64(size),
		true,
		0666,
	}
}
