package status

import (
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/gameap/daemon/internal/app/server/status"
)

func (suite *Suite) TestVersionSuccess() {
	suite.Auth(server.ModeStatus)

	r := suite.ClientWriteReadAndDecodeList([]interface{}{status.Version})

	suite.Require().Equal(response.StatusOK, response.Code(r[0].(uint8)))
}

func (suite *Suite) TestStatusBaseSuccess() {
	suite.Auth(server.ModeStatus)

	r := suite.ClientWriteReadAndDecodeList([]interface{}{status.StatusBase})

	suite.Require().Equal(response.StatusOK, response.Code(r[0].(uint8)))
}

func (suite *Suite) TestVersionAndStatusSuccess() {
	suite.Auth(server.ModeStatus)

	r1 := suite.ClientWriteReadAndDecodeList([]interface{}{status.Version})
	r2 := suite.ClientWriteReadAndDecodeList([]interface{}{status.StatusBase})

	suite.Require().Equal(response.StatusOK, response.Code(r1[0].(uint8)))
	suite.Require().Equal(response.StatusOK, response.Code(r2[0].(uint8)))
}
