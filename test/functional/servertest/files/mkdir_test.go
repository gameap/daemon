package files

import (
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestMakeDirSuccess() {
	suite.Auth(server.ModeFiles)
	rel, abs := suite.workPath("mkdir")
	msg := []interface{}{files.MakeDir, rel}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(abs)
}

func (suite *Suite) TestMakeDir_WhenThreeMessage_ExpectSuccess() {
	suite.Auth(server.ModeFiles)
	rel1, abs1 := suite.workPath("mkdir")
	rel2, abs2 := suite.workPath("mkdir")
	rel3, abs3 := suite.workPath("mkdir")
	msg1 := []interface{}{files.MakeDir, rel1}
	msg2 := []interface{}{files.MakeDir, rel2}
	msg3 := []interface{}{files.MakeDir, rel3}

	r1 := suite.ClientWriteReadAndDecodeList(msg1)
	r2 := suite.ClientWriteReadAndDecodeList(msg2)
	r3 := suite.ClientWriteReadAndDecodeList(msg3)

	suite.Equal(response.StatusOK, response.Code(r1[0].(uint8)))
	suite.DirExists(abs1)
	suite.Equal(response.StatusOK, response.Code(r2[0].(uint8)))
	suite.DirExists(abs2)
	suite.Equal(response.StatusOK, response.Code(r3[0].(uint8)))
	suite.DirExists(abs3)
}

func (suite *Suite) TestMakeDirInvalidMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.MakeDir, 122, "invalid"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Invalid message", r[1])
}
