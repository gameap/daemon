package files

import (
	"os"
	"strconv"
	"time"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestMakeDirSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir := os.TempDir() + "/files_test_" + strconv.Itoa(int(time.Now().UnixNano()))
	defer os.RemoveAll(tempDir)
	msg := []interface{}{files.MakeDir, tempDir}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(tempDir)
}

func (suite *Suite) TestMakeDirInvalidMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.MakeDir, 122, "invalid"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Invalid message", r[1])
}
