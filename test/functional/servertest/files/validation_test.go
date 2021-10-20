package files

import (
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestEmptyMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Invalid message", r[1])
}

func (suite *Suite) TestStringMessage() {
	suite.Auth(server.ModeFiles)
	msg := "strings"

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Failed to decode message", r[1])
}

func (suite *Suite) TestInvalidOperationCode() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{0xFF}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusError, response.Code(r[0].(uint8)))
	assert.Equal(suite.T(), "Invalid operation", r[1])
}
