package files

import (
	"os"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestListSuccess() {
	suite.Auth(server.ModeFiles)
	err := os.Chmod("../../../../test/files/file.txt", 0664)
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := []interface{}{files.ReadDir, "../../../../test/files", files.ListWithDetails}

	r := suite.ClientWriteReadAndDecodeList(msg)

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	fList := r[2].([]interface{})
	var fileTxtInfo []interface{}
	for _, item := range fList {
		fInfo, ok := item.([]interface{})
		if !ok {
			suite.T().Fatal("Invalid item")
		}

		if fInfo[0] == "file.txt" {
			fileTxtInfo = fInfo
		}
	}
	if fileTxtInfo == nil {
		suite.T().Fatal("file.txt not found")
	}
	assert.Equal(suite.T(), "file.txt", fileTxtInfo[0])
	assert.Equal(suite.T(), uint8(9), fileTxtInfo[1])
	assert.Equal(suite.T(), uint8(2), fileTxtInfo[3])
	assert.Equal(suite.T(), uint16(0664), fileTxtInfo[4])
}
