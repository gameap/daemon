package files

import (
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestTextFileInfoSuccess() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileInfo, "../../../../test/files/file.txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	assert.Equal(suite.T(), "file.txt", fInfo[0])
	assert.Equal(suite.T(), uint8(9), fInfo[1])
	assert.Equal(suite.T(), uint8(2), fInfo[2])
	assert.Equal(suite.T(), uint16(0664), fInfo[6])
	assert.Equal(suite.T(), "text/plain; charset=utf-8", fInfo[7])
}

func (suite *Suite) TestJsonFileInfoSuccess() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileInfo, "../../../../test/files/file.json"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	assert.Equal(suite.T(), "file.json", fInfo[0])
	assert.Equal(suite.T(), uint8(66), fInfo[1])
	assert.Equal(suite.T(), uint8(2), fInfo[2])
	assert.Equal(suite.T(), uint16(0664), fInfo[6])
	assert.Equal(suite.T(), "application/json", fInfo[7])
}

func (suite *Suite) TestDirectoryInfoSuccess() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileInfo, "../../../../test/files/directory"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	assert.Equal(suite.T(), "directory", fInfo[0])
	assert.Equal(suite.T(), uint16(4096), fInfo[1])
	assert.Equal(suite.T(), uint8(1), fInfo[2])
	assert.Equal(suite.T(), uint16(0775), fInfo[6])
	assert.Equal(suite.T(), "", fInfo[7])
}

func (suite *Suite) TestSymlinkInfoSuccess() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileInfo, "../../../../test/files/symlink_to_file_txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	fInfo := r[2].([]interface{})
	assert.Equal(suite.T(), "symlink_to_file_txt", fInfo[0])
	assert.Equal(suite.T(), uint8(10), fInfo[1])
	assert.Equal(suite.T(), uint8(6), fInfo[2])
	assert.Equal(suite.T(), uint16(0777), fInfo[6])
	assert.Equal(suite.T(), "", fInfo[7])
}
