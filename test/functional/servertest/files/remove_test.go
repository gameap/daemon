package files

import (
	"os"
	"path/filepath"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
)

func (suite *Suite) TestRemoveFileSuccess() {
	suite.Auth(server.ModeFiles)
	rel, abs := suite.workFile("rm", []byte("x"))
	msg := []interface{}{files.FileRemove, rel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoFileExists(abs)
}

func (suite *Suite) TestRemoveEmptyDirSuccess() {
	suite.Auth(server.ModeFiles)
	rel, abs := suite.workDir("rmdir")
	msg := []interface{}{files.FileRemove, rel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(abs)
}

func (suite *Suite) TestRemoveNotEmptyDirFail() {
	suite.Auth(server.ModeFiles)
	rel, abs := suite.workDir("rmdir")
	if err := os.MkdirAll(filepath.Join(abs, "inner_dir"), 0o755); err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileRemove, rel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Failed to remove", r[1].(string))
	suite.DirExists(abs)
}

func (suite *Suite) TestRemoveRecursiveNotEmptyDirSuccess() {
	suite.Auth(server.ModeFiles)
	rel, abs := suite.workDir("rmdir")
	if err := os.MkdirAll(filepath.Join(abs, "inner_dir"), 0o755); err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileRemove, rel, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(abs)
}

func (suite *Suite) TestNotExistFileFail() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileRemove, "/invalid-path", true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Path not exist", r[1].(string))
}
