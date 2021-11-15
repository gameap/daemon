package commands

import (
	"github.com/et-nik/binngo"
	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestAuth() {
	msg, err := binngo.Marshal([]interface{}{0, "login", "password", server.ModeCommands})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.Suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.Suite.ClientRead(buf)
	var r response.Response

	err = decode.Unmarshal(buf, &r)

	if assert.NoError(suite.T(), err) {
		assert.Equal(suite.T(), response.StatusOK, r.Code)
		assert.Equal(suite.T(), "Auth success", r.Info)
	}
}

func (suite *Suite) TestExecSuccess() {
	suite.Auth(server.ModeCommands)
	msg, err := binngo.Marshal([]interface{}{1, "echo -n \"test string\"", "/"})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var r []interface{}

	err = decode.Unmarshal(buf, &r)

	if assert.NoError(suite.T(), err) {
		assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
		assert.Equal(suite.T(), int8(0), r[1])
		assert.Equal(suite.T(), "test string", r[2])
	}
}

func (suite *Suite) TestExecErrorCode() {
	suite.Auth(server.ModeCommands)
	msg, err := binngo.Marshal([]interface{}{1, "false", "/"})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var r []interface{}

	err = decode.Unmarshal(buf, &r)

	if assert.NoError(suite.T(), err) {
		assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
		assert.Equal(suite.T(), uint8(1), r[1])
		assert.Equal(suite.T(), "", r[2])
	}
}

func (suite *Suite) TestInvalidCommand() {
	suite.Auth(server.ModeCommands)
	msg, err := binngo.Marshal([]interface{}{1, "invalid command", "/"})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var r []interface{}

	err = decode.Unmarshal(buf, &r)

	suite.Require().NoError(err)
	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Contains(r[1], "executable file not found")
}

func (suite *Suite) TestInvalidWorkDir() {
	suite.Auth(server.ModeCommands)
	msg, err := binngo.Marshal([]interface{}{1, "echo hello", "/invalid-path"})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var r []interface{}

	err = decode.Unmarshal(buf, &r)

	suite.Require().NoError(err)
	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Contains(r[1], "invalid work directory")
}

func (suite *Suite) TestInvalidMessage() {
	suite.Auth(server.ModeCommands)
	msg, err := binngo.Marshal(struct {
		invalid string
	}{
		invalid: "echo hello",
	})
	if err != nil {
		suite.T().Fatal(err)
	}
	suite.ClientWrite(msg)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	var r []interface{}

	err = decode.Unmarshal(buf, &r)

	suite.Require().NoError(err)
	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Failed to decode message", r[1])
}
