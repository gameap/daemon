package customhandlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gameap/daemon/internal/app/components/customhandlers"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const toolName = "install-tool.sh"

func sha256Of(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

// A tool that is already there, whatever its size, and the leftover of an
// interrupted download are both replaced by what the source serves: nothing is
// resumed onto them.
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

func Test_GetTool_BrokenDownloadKeepsThePreviousTool(t *testing.T) {
	previous := []byte("previous tool\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte("#!/bin/sh\n# the connection breaks off here"))
	}))
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
	assert.Contains(t, err.Error(), "failed to get tool")
	assert.Contains(t, err.Error(), "unexpected EOF")
	assert.Equal(t, int(domain.ErrorResult), code)
	got, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, previous, got, "half a tool must not take the place of the previous one")
	assert.NoFileExists(t, destination+".part")
}

// A source may say what its tool hashes to. A file that does not is complete
// and still not the tool: it does not take the place of the previous one.
func Test_GetTool_ToolThatDoesNotMatchItsChecksumKeepsThePreviousOne(t *testing.T) {
	previous := []byte("previous tool\n")
	expected := []byte("#!/bin/sh\n# the tool the link was written for\n")
	served := []byte("#!/bin/sh\n# what the server has under its name by now\n")

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), served, 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(srcDir, "SHA256SUMS"), []byte(sha256Of(expected)+"  "+toolName+"\n"), 0o644,
	))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	tests := []struct {
		name     string
		checksum string
	}{
		{name: "sum", checksum: sha256Of(expected)},
		{name: "sum with its type", checksum: "sha256:" + sha256Of(expected)},
		{name: "checksum file", checksum: "file:" + server.URL + "/SHA256SUMS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := t.TempDir()
			destination := filepath.Join(toolsDir, toolName)
			require.NoError(t, os.WriteFile(destination, previous, 0o600))
			handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})
			out := &bytes.Buffer{}

			code, err := handler.Handle(
				context.Background(),
				[]string{server.URL + "/" + toolName + "?checksum=" + tt.checksum},
				out,
				contracts.ExecutorOptions{},
			)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to get tool")
			assert.Contains(t, err.Error(), "checksum mismatch")
			assert.Contains(
				t, err.Error(),
				"sha256 "+sha256Of(expected)+" is expected and the file has "+sha256Of(served),
			)
			assert.Equal(t, int(domain.ErrorResult), code)
			got, err := os.ReadFile(destination)
			require.NoError(t, err)
			assert.Equal(t, previous, got, "a tool that is not the expected one must not take the place of the previous one")
			assert.NoFileExists(t, destination+".part")
		})
	}
}

// A tool is a file the source serves, stored as it is served. Nothing but a
// link to a file is understood, and nothing is unpacked: a source can say what
// its file hashes to, and cannot ask for it to be unpacked.
func Test_GetTool_Source(t *testing.T) {
	archive := []byte("\x1f\x8b\x08 not unpacked")

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "tool.tar.gz"), archive, 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	tests := []struct {
		name      string
		source    string
		wantTool  string
		wantError string
	}{
		{
			name:     "archive is stored as it is",
			source:   server.URL + "/tool.tar.gz",
			wantTool: "tool.tar.gz",
		},
		{
			name:     "query is not part of the name",
			source:   server.URL + "/tool.tar.gz?version=2",
			wantTool: "tool.tar.gz",
		},
		{
			name:     "checksum the tool has",
			source:   server.URL + "/tool.tar.gz?checksum=sha256:" + sha256Of(archive),
			wantTool: "tool.tar.gz",
		},
		{
			name:     "source that says its file is not unpacked",
			source:   server.URL + "/tool.tar.gz?archive=false&version=2",
			wantTool: "tool.tar.gz",
		},
		{
			name:      "source that asks for its file to be unpacked",
			source:    server.URL + "/tool.tar.gz?archive=tar.gz",
			wantError: `archive parameter "tar.gz" of the source is not supported, a tool is stored as it is served`,
		},
		{
			name:      "source that asks for its file to be unpacked as whatever it is",
			source:    server.URL + "/tool.tar.gz?archive=true",
			wantError: `archive parameter "true" of the source is not supported`,
		},
		{
			name:      "checksum that cannot be one",
			source:    server.URL + "/tool.tar.gz?checksum=sha256:abcdef",
			wantError: "invalid checksum parameter of the source: a sha256 checksum has 64 hex digits, 6 are given",
		},
		{
			name:      "ftp",
			source:    "ftp://tools.example.com/install-tool.sh",
			wantError: "unsupported source, a direct http or https link to a file is expected",
		},
		{
			name:      "local path",
			source:    filepath.Join(srcDir, "tool.tar.gz"),
			wantError: "unsupported source, a direct http or https link to a file is expected",
		},
		{
			name:      "git repository",
			source:    "git::https://github.com/example/tool.git",
			wantError: "unsupported source, a direct http or https link to a file is expected",
		},
		{
			name:      "link that names no file",
			source:    server.URL + "/",
			wantError: "names no file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := filepath.Join(t.TempDir(), "tools")
			handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})
			out := &bytes.Buffer{}

			code, err := handler.Handle(context.Background(), []string{tt.source}, out, contracts.ExecutorOptions{})

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Equal(t, int(domain.ErrorResult), code)
				assert.NoDirExists(t, toolsDir, "nothing is created for a source that is refused")

				return
			}

			require.NoError(t, err, out.String())
			assert.Equal(t, int(domain.SuccessResult), code)

			got, err := os.ReadFile(filepath.Join(toolsDir, tt.wantTool))
			require.NoError(t, err)
			assert.Equal(t, archive, got)

			entries, err := os.ReadDir(toolsDir)
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

// The output of the task names the source without what gives access to it: the
// password of the link and its query.
func Test_GetTool_OutputShowsTheSourceWithoutWhatGivesAccessToIt(t *testing.T) {
	published := []byte("#!/bin/sh\n")

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, toolName), published, 0o644))
	server := httptest.NewServer(http.FileServer(http.Dir(srcDir)))
	defer server.Close()

	withCredentials := strings.Replace(server.URL, "http://", "http://repo:secret@", 1)
	shown := strings.Replace(server.URL, "http://", "http://repo:xxxxx@", 1)

	tests := []struct {
		name     string
		file     string
		checksum string
		wantCode domain.Result
	}{
		{
			name:     "tool that is stored",
			file:     toolName,
			checksum: sha256Of(published),
			wantCode: domain.SuccessResult,
		},
		{
			name:     "tool that is not there",
			file:     "missing-tool.sh",
			checksum: sha256Of(published),
			wantCode: domain.ErrorResult,
		},
		{
			name:     "tool that does not match its checksum",
			file:     toolName,
			checksum: sha256Of([]byte("another tool")),
			wantCode: domain.ErrorResult,
		},
		{
			name:     "checksum file that is not there",
			file:     toolName,
			checksum: "file:" + withCredentials + "/SHA256SUMS?key=secret-key",
			wantCode: domain.ErrorResult,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolsDir := t.TempDir()
			handler := customhandlers.NewGetTool(&config.Config{ToolsPath: toolsDir})
			out := &bytes.Buffer{}

			code, err := handler.Handle(
				context.Background(),
				[]string{withCredentials + "/" + tt.file + "?token=secret-token&checksum=" + tt.checksum},
				out,
				contracts.ExecutorOptions{},
			)

			assert.Equal(t, int(tt.wantCode), code)
			assert.Contains(t, out.String(), "Getting tool from "+shown+"/"+tt.file+" to ")
			assert.NotContains(t, out.String(), "secret")
			assert.NotContains(t, out.String(), "token")

			if tt.wantCode == domain.SuccessResult {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}

func Test_GetTool_NoSource(t *testing.T) {
	handler := customhandlers.NewGetTool(&config.Config{ToolsPath: t.TempDir()})

	code, err := handler.Handle(context.Background(), nil, &bytes.Buffer{}, contracts.ExecutorOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source provided")
	assert.Equal(t, int(domain.ErrorResult), code)
}
