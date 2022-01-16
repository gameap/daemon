package files

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/pkg/sys"
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

func (suite *Suite) assertFileDetails(
	fInfo []interface{},
	name string,
	size uint64,
	fileType files.FileType,
	permissions uint16,
	mime string,
) {
	suite.T().Helper()

	// file name
	suite.Equal(name, fInfo[0])

	if runtime.GOOS == "windows" && fileType == files.TypeSymlink {
		suite.T().Log("ignore symlink assertion in windows")
		return
	}

	// file size
	if fileType != files.TypeDir {
		switch v := fInfo[1].(type) {
		case uint8:
			suite.Equal(size, uint64(v))
		case uint16:
			suite.Equal(size, uint64(v))
		case uint32:
			suite.Equal(size, uint64(v))
		case uint64:
			suite.Equal(size, v)
		}
	}

	// file type (file, directory, ...)
	suite.Equal(uint8(fileType), fInfo[2])

	// permissions
	if runtime.GOOS != sys.Windows {
		suite.Equal(permissions, fInfo[6])
	}

	// mime type
	suite.Equal(mime, fInfo[7])
}

func (suite *Suite) assertUploadedFileContents(expected []byte) {
	suite.T().Helper()

	contents, err := os.ReadFile(suite.tempFileDestination)
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.Equal(expected, contents)
}

func (suite *Suite) assertFirstAndLastFileBytes(first []byte, last []byte) {
	suite.T().Helper()

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
	suite.T().Helper()

	return []interface{}{
		files.FileSend,
		files.GetFileFromClient,
		suite.tempFileDestination,
		uint64(size),
		true,
		0666,
	}
}
