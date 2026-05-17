package files

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
)

func (suite *Suite) TestMoveFileSuccess() {
	suite.Auth(server.ModeFiles)
	srcRel, srcAbs := suite.workFile("move", []byte("data"))
	dstRel, dstAbs := suite.workPath("moved")
	msg := []interface{}{files.FileMove, srcRel, dstRel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.FileExists(dstAbs)
	suite.NoFileExists(srcAbs)
}

func (suite *Suite) TestCopyFileSuccess() {
	suite.Auth(server.ModeFiles)
	srcRel, srcAbs := suite.workFile("copy", []byte("data"))
	dstRel, dstAbs := suite.workPath("copied")
	msg := []interface{}{files.FileMove, srcRel, dstRel, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.FileExists(dstAbs)
	suite.FileExists(srcAbs)
}

func (suite *Suite) TestCopyRelativePathSuccess() {
	suite.Auth(server.ModeFiles)
	dstRel, dstAbs := suite.workPath("copytree")
	msg := []interface{}{files.FileMove, fixturesRel, dstRel, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.DirExists(filepath.Join(dstAbs, "directory"))
	suite.FileExists(filepath.Join(dstAbs, "file.json"))
	suite.FileExists(filepath.Join(dstAbs, "file.txt"))
	if runtime.GOOS != "windows" && suite.FileExists(filepath.Join(dstAbs, "symlink_to_file_txt")) {
		s, err := os.Lstat(filepath.Join(dstAbs, "symlink_to_file_txt"))
		if err != nil {
			suite.T().Fatal(err)
		}
		suite.True(s.Mode()&fs.ModeSymlink != 0)
	}
}

func (suite *Suite) TestCopyDirectorySuccess() {
	suite.Auth(server.ModeFiles)
	srcRel, srcAbs := suite.workDir("src")
	if err := os.WriteFile(filepath.Join(srcAbs, "f.bin"), []byte("z"), 0o644); err != nil {
		suite.T().Fatal(err)
	}
	dstRel, dstAbs := suite.workPath("dst")
	msg := []interface{}{files.FileMove, srcRel, dstRel, true}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.FileExists(filepath.Join(dstAbs, "f.bin"))
	suite.FileExists(filepath.Join(srcAbs, "f.bin"))
}

func (suite *Suite) TestMoveDirectorySuccess() {
	suite.Auth(server.ModeFiles)
	srcRel, srcAbs := suite.workDir("src")
	if err := os.WriteFile(filepath.Join(srcAbs, "f.bin"), []byte("z"), 0o644); err != nil {
		suite.T().Fatal(err)
	}
	dstRel, dstAbs := suite.workPath("dst")
	msg := []interface{}{files.FileMove, srcRel, dstRel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusOK, response.Code(r[0].(uint8)))
	suite.NoDirExists(srcAbs)
	suite.FileExists(filepath.Join(dstAbs, "f.bin"))
}

func (suite *Suite) TestMoveInvalidSource() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileMove, "/invalid-source", "/invalid-destination", false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Source \"/invalid-source\" not found", r[1].(string))
}

func (suite *Suite) TestMoveInvalidDestination() {
	suite.Auth(server.ModeFiles)
	srcRel, _ := suite.workDir("src")
	dstRel, _ := suite.workDir("dst")
	msg := []interface{}{files.FileMove, srcRel, dstRel, false}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Destination \""+dstRel+"\" already exists", r[1].(string))
}

func (suite *Suite) TestMoveInvalidMessage() {
	suite.Auth(server.ModeFiles)
	msg := []interface{}{files.FileMove, 0xFF}

	r := suite.ClientWriteReadAndDecodeList(msg)

	suite.Equal(response.StatusError, response.Code(r[0].(uint8)))
	suite.Equal("Invalid message", r[1].(string))
}
