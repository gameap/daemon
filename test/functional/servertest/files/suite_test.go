package files

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/test/functional/servertest"
	"github.com/stretchr/testify/suite"
)

// The legacy TCP file handler is now jailed to the daemon work directory
// (servertest.Suite.WorkPath). Every path sent to the daemon is therefore
// resolved relative to WorkPath, so the fixtures these tests operate on are
// mirrored into WorkPath/testfiles and the helpers below hand out
// (relative-to-WorkPath, absolute-on-disk) path pairs.
const fixturesRel = "testfiles"

type Suite struct {
	servertest.Suite

	seq int

	relFileDestination  string
	tempFileDestination string
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}

func (suite *Suite) SetupSuite() {
	suite.Suite.SetupSuite()

	err := fsutil.Copy(
		"../../../../test/files",
		filepath.Join(suite.WorkPath, fixturesRel),
		fsutil.CopyOptions{Symlink: fsutil.SymlinkShallow},
	)
	if err != nil {
		suite.T().Fatal(err)
	}
}

func (suite *Suite) SetupTest() {
	suite.Suite.SetupTest()

	suite.relFileDestination = "upload/file"
	suite.tempFileDestination = filepath.Join(suite.WorkPath, "upload", "file")
}

func (suite *Suite) TearDownSuite() {
	suite.Suite.TearDownSuite()
}

// workPath returns a unique (relative-to-WorkPath, absolute-on-disk) path pair.
// The relative form is what the client sends to the jailed daemon.
func (suite *Suite) workPath(name string) (rel, abs string) {
	suite.T().Helper()
	suite.seq++
	rel = "ft/" + name + "_" + strconv.Itoa(suite.seq)
	abs = filepath.Join(suite.WorkPath, filepath.FromSlash(rel))

	return rel, abs
}

// workDir creates a unique directory inside WorkPath and returns its
// (relative, absolute) path pair.
func (suite *Suite) workDir(name string) (rel, abs string) {
	suite.T().Helper()
	rel, abs = suite.workPath(name)
	if err := os.MkdirAll(abs, 0o755); err != nil {
		suite.T().Fatal(err)
	}

	return rel, abs
}

// workFile creates a unique file (with its parent directory) inside WorkPath
// and returns its (relative, absolute) path pair.
func (suite *Suite) workFile(name string, contents []byte) (rel, abs string) {
	suite.T().Helper()
	rel, abs = suite.workPath(name)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		suite.T().Fatal(err)
	}
	if err := os.WriteFile(abs, contents, 0o644); err != nil {
		suite.T().Fatal(err)
	}

	return rel, abs
}

func (suite *Suite) fixtureAbs(name string) string {
	suite.T().Helper()

	return filepath.Join(suite.WorkPath, fixturesRel, name)
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
		//nolint:gocritic
		switch fInfo[1].(type) {
		case uint8:
			suite.Equal(size, uint64(fInfo[1].(uint8)))
		case uint16:
			suite.Equal(size, uint64(fInfo[1].(uint16)))
		case uint32:
			suite.Equal(size, uint64(fInfo[1].(uint32)))
		case uint64:
			suite.Equal(size, fInfo[1].(uint64))
		}
	}

	// file type (file, directory, ...)
	suite.Equal(uint8(fileType), fInfo[2])

	// permissions. A symlink's own lstat permission bits are only
	// deterministic on Linux (always 0777); macOS reports the real bits, so
	// the strict check is limited to where it is meaningful.
	if runtime.GOOS != "windows" && (fileType != files.TypeSymlink || runtime.GOOS == "linux") {
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
		suite.relFileDestination,
		uint64(size),
		true,
		0666,
	}
}
