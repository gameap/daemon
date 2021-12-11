package files

import (
	"os"

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

func (suite *Suite) TestDownloadMultipleSuccess() {
	// First File
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileSend, files.SendFileToClient, "../../../../test/files/file.txt"}
	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))
	suite.Equal("File is ready to transfer", r[1].(string))
	suite.Equal(uint8(9), r[2].(uint8))

	buf := make([]byte, 9)
	suite.ClientRead(buf)

	suite.Equal("file.txt\n", string(buf))

	// Second File
	msg = []interface{}{files.FileSend, files.SendFileToClient, "../../../../test/files/file2.txt"}
	r = suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))
	suite.Equal("File is ready to transfer", r[1].(string))
	suite.Equal(uint8(10), r[2].(uint8))

	buf2 := make([]byte, 10)
	suite.ClientRead(buf2)

	suite.Equal("file2.txt\n", string(buf2))
}

func (suite *Suite) TestDownload_EmptyFile_Success() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileSend, files.SendFileToClient, "../../../../test/files/empty_file.txt"}
	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))
	suite.Equal("File is ready to transfer", r[1].(string))
	suite.Equal(uint8(0), r[2].(uint8))

	// Transfer the file
	buf := make([]byte, 0)
	suite.ClientRead(buf)

	suite.Equal("", string(buf))
}

func (suite *Suite) TestUploadSuccess() {
	suite.Authenticate()
	fileContents := []byte{'f', 'i', 'l', 'e', 'c', 'o', 'n', 't', 'e', 'n', 't', 's'}
	msg := suite.givenUploadMessage(len(fileContents))
	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))

	// Transfer the file
	suite.ClientFileContentsWrite(fileContents)
	r = suite.readMessageFromClient()

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	suite.assertUploadedFileSize(len(fileContents))
	suite.assertUploadedFileContents(fileContents)
}

func (suite *Suite) TestUploadBigFileSuccess() {
	suite.Authenticate()
	msg := suite.givenUploadMessage(1000000)
	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))

	// Transfer the file
	for i := 0; i < 100000; i++ {
		suite.ClientFileContentsWrite([]byte(`_big_file_`))
	}
	r = suite.readMessageFromClient()

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	suite.Require().FileExists(suite.tempFileDestination)
	suite.assertUploadedFileSize(1000000)
	suite.assertFirstAndLastFileBytes([]byte(`_big_file__big_file_`), []byte(`_big_file__big_file_`))
}

func (suite *Suite) TestUploadRaccoonSuccess() {
	suite.Authenticate()
	fileContents, err := os.ReadFile("../../../files/raccoon.jpg")
	if err != nil {
		suite.T().Fatal(err)
	}
	msg := suite.givenUploadMessage(len(fileContents))
	r := suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))

	suite.ClientFileContentsWrite(fileContents)
	r = suite.readMessageFromClient()

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	suite.assertUploadedFileSize(len(fileContents))
	suite.assertUploadedFileContents(fileContents)
}

func (suite *Suite) TestUpload_WhenListDirectoryCommandExecutedBefore_Success() {
	suite.Authenticate()
	// Read Directory
	readDirMsg := []interface{}{files.ReadDir, "../../../../test/files", files.ListWithDetails}
	r := suite.ClientWriteReadAndDecodeList(readDirMsg)
	suite.Require().Equal(response.StatusOK, response.Code(r[0].(uint8)))
	// File arrange
	fileContents := []byte(`filecontents`)
	msg := suite.givenUploadMessage(len(fileContents))
	r = suite.ClientWriteReadAndDecodeList(msg)
	suite.Equal(response.StatusReadyToTransfer, response.Code(r[0].(uint8)))

	suite.ClientFileContentsWrite(fileContents)
	r = suite.readMessageFromClient()

	assert.Equal(suite.T(), response.StatusOK, response.Code(r[0].(uint8)))
	suite.assertUploadedFileSize(len(fileContents))
	suite.assertUploadedFileContents(fileContents)
}
