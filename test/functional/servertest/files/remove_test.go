package files

import (
	"os"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
)

func (suite *Suite) TestRemoveFileSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp(os.TempDir(), "files_test_")
	tempFile, _ := os.CreateTemp(tempDir, "file")
	tempFileName := tempFile.Name()
	err := tempFile.Close()
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileRemove, tempFileName, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoFileExists(tempFileName)
}

func (suite *Suite) TestRemoveEmptyDirSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	msg := []interface{}{files.FileRemove, tempDir, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(tempDir)
}

func (suite *Suite) TestRemoveNotEmptyDirFail() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	_, _ = os.MkdirTemp(tempDir, "inner_dir")
	msg := []interface{}{files.FileRemove, tempDir, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Failed to remove", r[1].(string))
	suite.DirExists(tempDir)
}

func (suite *Suite) TestRemoveRecursiveNotEmptyDirSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	_, _ = os.MkdirTemp(tempDir, "inner_dir")
	msg := []interface{}{files.FileRemove, tempDir, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(tempDir)
}

func (suite *Suite) TestNotExistFileFail() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileRemove, "/invalid-path", true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Path not exist", r[1].(string))
}
