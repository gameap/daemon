package files

import (
	"os"
	"testing"

	"github.com/gameap/daemon/internal/app/server"
	"github.com/gameap/daemon/internal/app/server/files"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/stretchr/testify/assert"
)

func (suite *Suite) TestChmodSuccess() {
	suite.Auth(server.ModeFiles)
	tempDir, _ := os.MkdirTemp("", "files_test_")
	tempFile, _ := os.CreateTemp(tempDir, "file")

	tests := []struct {
		name string
		perm uint16
		expected string
	}{
		{"only owner read write", 0600, "-rw-------"},
		{"only owner read", 0400, "-r--------"},
		{"all read write", 0666, "-rw-rw-rw-"},
		{"all read write execute", 0777, "-rwxrwxrwx"},
	}
	for _, test := range tests {
		suite.T().Run(test.name, func(t *testing.T) {
			msg := []interface{}{files.FileChmod, tempFile.Name(), test.perm}

			r := suite.ClientWriteReadAndDecodeList(msg)

			assert.Equal(t, response.StatusOK, response.Code(r[0].(uint8)))
			stat, err := os.Stat(tempFile.Name())
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, test.expected, stat.Mode().String())
		})
	}
}
