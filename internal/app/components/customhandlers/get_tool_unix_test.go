//go:build linux || darwin

package customhandlers_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GetTool_ToolIsExecutableByItsOwnerOnly(t *testing.T) {
	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), []byte("#!/bin/sh\n"), 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	toolsDir := t.TempDir()
	handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})

	code, err := handler.Handle(
		context.Background(),
		[]string{server.URL + "/" + toolName},
		&bytes.Buffer{},
		contracts.ExecutorOptions{},
	)

	require.NoError(t, err)
	assert.Equal(t, int(domain.SuccessResult), code)

	info, err := os.Lstat(filepath.Join(toolsDir, toolName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode())
}

// The tool is downloaded under a name that can be told in advance. A symlink
// waiting under that name, or under the name of the tool, is replaced: the
// download is not written to where it leads.
func Test_GetTool_SymlinkInPlaceOfTheDownload(t *testing.T) {
	published := []byte("#!/bin/sh\necho tool\n")

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), published, 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	tests := []struct {
		name string
		// link is the name the symlink stands under, relative to the tools
		// directory.
		link string
		// missing makes the link lead to a file that is not there.
		missing bool
	}{
		{name: "link to a file under the name of the partial download", link: toolName + ".part"},
		{name: "link to a missing file under the name of the partial download", link: toolName + ".part", missing: true},
		{name: "link to a file under the name of the tool", link: toolName},
		{name: "link to a missing file under the name of the tool", link: toolName, missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := t.TempDir()
			outside := t.TempDir()
			target := filepath.Join(outside, "passwd")
			if !tt.missing {
				require.NoError(t, os.WriteFile(target, []byte("root:x:0:0"), 0o600))
			}
			require.NoError(t, os.Symlink(target, filepath.Join(toolsDir, tt.link)))
			handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})
			out := &bytes.Buffer{}

			code, err := handler.Handle(
				context.Background(),
				[]string{server.URL + "/" + toolName},
				out,
				contracts.ExecutorOptions{},
			)

			require.NoError(t, err, out.String())
			assert.Equal(t, int(domain.SuccessResult), code)

			if tt.missing {
				assert.NoFileExists(t, target, "nothing is created where the link leads")
			} else {
				content, err := os.ReadFile(target)
				require.NoError(t, err)
				assert.Equal(t, "root:x:0:0", string(content), "the file the link leads to keeps its content")

				info, err := os.Lstat(target)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode(), "the file the link leads to keeps its mode")
			}

			info, err := os.Lstat(filepath.Join(toolsDir, toolName))
			require.NoError(t, err)
			require.True(t, info.Mode().IsRegular(), "the tool has to be a regular file, it is %s", info.Mode())

			got, err := os.ReadFile(filepath.Join(toolsDir, toolName))
			require.NoError(t, err)
			assert.Equal(t, published, got)
			assert.NoFileExists(t, filepath.Join(toolsDir, toolName+".part"))
		})
	}
}
