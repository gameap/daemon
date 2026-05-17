package files

import (
	"os"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
)

func (suite *Suite) TestTextFileInfoSuccess() {
	suite.Auth(server.ModeFiles)
	err := os.Chmod(suite.fixtureAbs("file.txt"), 0664)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileInfo, fixturesRel + "/file.txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	suite.assertFileDetails(
		fInfo,
		"file.txt",
		9,
		files.TypeFile,
		0664,
		"text/plain; charset=utf-8",
	)
}

func (suite *Suite) TestJsonFileInfoSuccess() {
	suite.Auth(server.ModeFiles)
	err := os.Chmod(suite.fixtureAbs("file.json"), 0664)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileInfo, fixturesRel + "/file.json"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	suite.assertFileDetails(
		fInfo,
		"file.json",
		66,
		files.TypeFile,
		0664,
		"application/json",
	)
}

func (suite *Suite) TestDirectoryInfoSuccess() {
	suite.Auth(server.ModeFiles)
	err := os.Chmod(suite.fixtureAbs("directory"), 0775)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileInfo, fixturesRel + "/directory"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	suite.assertFileDetails(
		fInfo,
		"directory",
		0,
		files.TypeDir,
		0775,
		"",
	)
}

func (suite *Suite) TestSymlinkInfoSuccess() {
	suite.Auth(server.ModeFiles)
	err := os.Chmod(suite.fixtureAbs("symlink_to_file_txt"), 0777)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileInfo, fixturesRel + "/symlink_to_file_txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	suite.assertFileDetails(
		fInfo,
		"symlink_to_file_txt",
		10,
		files.TypeSymlink,
		0777,
		"",
	)
}

func (suite *Suite) TestFileInfo_EmptyFile_Success() {
	suite.Authenticate()
	err := os.Chmod(suite.fixtureAbs("empty_file.txt"), 0664)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.FileInfo, fixturesRel + "/empty_file.txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo, ok := r[2].([]interface{})
	suite.Require().True(ok)
	suite.assertFileDetails(
		fInfo,
		"empty_file.txt",
		0,
		files.TypeFile,
		0664,
		"",
	)
}
