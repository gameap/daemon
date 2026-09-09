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

// http.FileServer answers HEAD with Accept-Ranges and serves Range requests,
// which is what lets go-getter resume onto a file that is already there.
func Test_GetTool_ReplacesThePreviousTool(t *testing.T) {
	const toolName = "install-tool.sh"
	published := bytes.Repeat([]byte("published line\n"), 100)

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), published, 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	tests := []struct {
		name     string
		existing []byte
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := t.TempDir()
			destination := filepath.Join(toolsDir, toolName)
			if tt.existing != nil {
				require.NoError(t, os.WriteFile(destination, tt.existing, 0o600))
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
		})
	}
}
