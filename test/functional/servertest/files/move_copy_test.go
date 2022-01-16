package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/gameap/daemon/pkg/sys"
)

func (suite *Suite) TestMoveFileSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	tempFile, _ := os.CreateTemp(tempDir, "file")
	tempFileName := tempFile.Name()
	_ = tempFile.Close()
	newFile := tempDir + "/newFile"
	msg := []interface{}{files.FileMove, tempFileName, newFile, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(tempDir)
	suite.FileExists(newFile)
	suite.NoFileExists(tempFile.Name())
}

func (suite *Suite) TestCopyFileSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	defer os.RemoveAll(tempDir)
	tempFile, _ := os.CreateTemp(tempDir, "file")
	newFile := tempDir + "/newFile"
	msg := []interface{}{files.FileMove, tempFile.Name(), newFile, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(tempDir)
	suite.FileExists(newFile)
	suite.FileExists(tempFile.Name())
}

func (suite *Suite) TestCopyRelativePathSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir := os.TempDir() + "/files_test_" + strconv.Itoa(int(time.Now().UnixNano()))
	msg := []interface{}{files.FileMove, "../../../../test/files", tempDir, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(tempDir + "/directory")
	suite.FileExists(tempDir + "/file.json")
	suite.FileExists(tempDir + "/file.txt")
	if runtime.GOOS != sys.Windows && suite.FileExists(tempDir+"/symlink_to_file_txt") {
		s, err := os.Lstat(tempDir + "/symlink_to_file_txt")
		if err != nil {
			suite.T().Fatal(err)
		}
		suite.True(s.Mode()&fs.ModeSymlink != 0)
	}
}

func (suite *Suite) TestCopyDirectorySuccess() {
	suite.Auth(server.ModeFiles)
	tempDirSource, _ := os.MkdirTemp("", "files_test_source_")
	defer os.RemoveAll(tempDirSource)
	tempDirDestination := os.TempDir() + "/files_test_destination_" + strconv.Itoa(int(time.Now().UnixNano()))
	defer os.RemoveAll(tempDirDestination)
	tempFile, _ := os.CreateTemp(tempDirSource, "file")
	tempFile.Close()
	msg := []interface{}{files.FileMove, tempDirSource, tempDirDestination, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.FileExists(tempDirSource + "/" + filepath.Base(tempFile.Name()))
	suite.FileExists(tempDirDestination + "/" + filepath.Base(tempFile.Name()))
}

func (suite *Suite) TestMoveDirectorySuccess() {
	suite.Auth(server.ModeFiles)
	tempDirSource, _ := os.MkdirTemp("", "files_test_source_")
	tempDirDestination := os.TempDir() + "/files_test_destination_" + strconv.Itoa(int(time.Now().UnixNano()))
	defer os.RemoveAll(tempDirDestination)
	tempFile, _ := os.CreateTemp(tempDirSource, "file")
	tempFileName := tempFile.Name()
	_ = tempFile.Close()
	msg := []interface{}{files.FileMove, tempDirSource, tempDirDestination, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(tempDirSource)
	suite.NoFileExists(tempDirSource + "/" + filepath.Base(tempFileName))
	suite.FileExists(tempDirDestination + "/" + filepath.Base(tempFileName))
}

func (suite *Suite) TestMoveInvalidSource() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileMove, "/invalid-source", "/invalid-destination", false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Source \"/invalid-source\" not found", r[1].(string))
}

func (suite *Suite) TestMoveInvalidDestination() {
	suite.Auth(server.ModeFiles)
	tempDirSource, _ := os.MkdirTemp("", "files_test_source_")
	defer os.RemoveAll(tempDirSource)
	tempDirDestination, _ := os.MkdirTemp("", "files_test_destination_")
	defer os.RemoveAll(tempDirDestination)
	msg := []interface{}{files.FileMove, tempDirSource, tempDirDestination, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Destination \""+tempDirDestination+"\" already exists", r[1].(string))
}

func (suite *Suite) TestMoveInvalidMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileMove, 0xFF}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Invalid message", r[1].(string))
}
