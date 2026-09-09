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

const toolName = "install-tool.sh"

// http.FileServer answers HEAD with Accept-Ranges and serves Range requests,
// which is what lets go-getter resume onto a file that is already there.
func Test_GetTool_ReplacesThePreviousTool(t *testing.T) {
	published := bytes.Repeat([]byte("published line\n"), 100)

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), published, 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	tests := []struct {
		name     string
		existing []byte
		partial  []byte
	}{
		{
			name: "no previous tool",
		},
		{
			name:     "smaller previous tool",
			existing: bytes.Repeat([]byte("previous line\n"), 10),
		},
		{
			name:     "larger previous tool",
			existing: bytes.Repeat([]byte("previous line\n"), 200),
		},
		{
			name:     "leftover of an interrupted download",
			existing: bytes.Repeat([]byte("previous line\n"), 10),
			partial:  bytes.Repeat([]byte("interrupted line\n"), 10),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := t.TempDir()
			destination := filepath.Join(toolsDir, toolName)
			if tt.existing != nil {
				require.NoError(t, os.WriteFile(destination, tt.existing, 0o600))
			}
			if tt.partial != nil {
				require.NoError(t, os.WriteFile(destination+".part", tt.partial, 0o600))
			}
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
			got, err := os.ReadFile(destination)
			require.NoError(t, err)
			assert.Equal(t, published, got)
			assert.NoFileExists(t, destination+".part")
		})
	}
}

func Test_GetTool_KeepsThePreviousToolWhenTheDownloadFails(t *testing.T) {
	previous := []byte("previous tool\n")

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	toolsDir := t.TempDir()
	destination := filepath.Join(toolsDir, toolName)
	require.NoError(t, os.WriteFile(destination, previous, 0o600))
	handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})
	out := &bytes.Buffer{}

	code, err := handler.Handle(
		context.Background(),
		[]string{server.URL + "/" + toolName},
		out,
		contracts.ExecutorOptions{},
	)

	require.Error(t, err)
	assert.Equal(t, int(domain.ErrorResult), code)
	got, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, previous, got, "the previous tool must survive a failed fetch")
	assert.NoFileExists(t, destination+".part")
}
