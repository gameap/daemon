package files

import (
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestMakeDirSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir := filepath.Join(os.TempDir(), "files_test_", strconv.Itoa(int(time.Now().UnixNano()))) //nolint:goconst
	defer os.RemoveAll(tempDir)
	msg := []interface{}{files.MakeDir, tempDir}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(tempDir)
}

func (suite *Suite) TestMakeDir_WhenThreeMessage_ExpectSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir1 := filepath.Join(os.TempDir(), "files_test", strconv.Itoa(int(time.Now().UnixNano())))
	tempDir2 := filepath.Join(os.TempDir(), "files_test", strconv.Itoa(int(time.Now().UnixNano())))
	tempDir3 := filepath.Join(os.TempDir(), "files_test", strconv.Itoa(int(time.Now().UnixNano())))
	defer os.RemoveAll(tempDir1)
	defer os.RemoveAll(tempDir2)
	defer os.RemoveAll(tempDir3)
	msg1 := []interface{}{files.MakeDir, tempDir1}
	msg2 := []interface{}{files.MakeDir, tempDir2}
	msg3 := []interface{}{files.MakeDir, tempDir3}

	r1 := suite.ClientWriteReadAndDecodeList(msg1)
	r2 := suite.ClientWriteReadAndDecodeList(msg2)
	r3 := suite.ClientWriteReadAndDecodeList(msg3)

	suite.Equal(response.StatusOK, response.Code(r1[0].(uint8)))
	suite.DirExists(tempDir1)
	suite.Equal(response.StatusOK, response.Code(r2[0].(uint8)))
	suite.DirExists(tempDir2)
	suite.Equal(response.StatusOK, response.Code(r3[0].(uint8)))
	suite.DirExists(tempDir3)
}

func (suite *Suite) TestMakeDirInvalidMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.MakeDir, 122, "invalid"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Invalid message", r[1])
}
