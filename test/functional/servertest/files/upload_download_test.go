package files

import (
	"os"
	"strconv"
	"time"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestDownloadSuccess() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileSend, files.SendFileToClient, "../../../../test/files/file.txt"}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))
	suite.Equal("File is ready to transfer", r[1].(string))
	suite.Equal(uint8(9), r[2].(uint8))

	// Transfer the file
	buf := make([]byte, 9)

	suite.ClientRead(buf)

	suite.Equal("file.txt\n", string(buf))
}

func (suite *Suite) TestUploadSuccess() {
	suite.Auth(server.ModeFiles)
	tempDirDestination := os.TempDir() + "/files_test_destination_" + strconv.Itoa(int(time.Now().UnixNano()))
	fileDestination := tempDirDestination + "/file"
	msg := []interface{}{
		files.FileSend,
		files.GetFileFromClient,
		fileDestination,
		uint64(12),
		true,
		0666,
	}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))

	// Transfer the file
	fileContents := []byte{'f', 'i', 'l', 'e', 'c', 'o', 'n', 't', 'e', 'n', 't', 's'}

	suite.ClientWrite(fileContents)
	buf := make([]byte, 256)
	suite.ClientRead(buf)
	r = []interface{}{}
	err := decode.Unmarshal(buf, &r)
	if err != nil {
		suite.T().Fatal(err)
	}

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	if suite.FileExists(fileDestination) {
		contents, err := os.ReadFile(fileDestination)
		if err != nil {
			suite.T().Fatal(err)
		}
		suite.Equal(fileContents, contents)
	}
}
