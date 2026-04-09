package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteEnrollConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gameap-daemon.yaml")

	cfg := &EnrollConfig{
		NodeID:               42,
		APIKey:               "test-api-key",
		ListenIP:             "0.0.0.0",
		ListenPort:           31717,
		CACertificateFile:    "/etc/gameap-daemon/certs/ca.crt",
		CertificateChainFile: "/etc/gameap-daemon/certs/server.crt",
		PrivateKeyFile:       "/etc/gameap-daemon/certs/server.key",
		WorkPath:             "/srv/gameap",
		LogLevel:             "info",
		GRPC: EnrollGRPC{
			Enabled: true,
			Address: "panel.example.com:31718",
		},
	}

	err := WriteEnrollConfig(path, cfg)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "ds_id: 42")
	assert.Contains(t, content, "api_key: test-api-key")
	assert.Contains(t, content, "listen_ip: 0.0.0.0")
	assert.Contains(t, content, "listen_port: 31717")
	assert.Contains(t, content, "ca_certificate_file: /etc/gameap-daemon/certs/ca.crt")
	assert.Contains(t, content, "certificate_chain_file: /etc/gameap-daemon/certs/server.crt")
	assert.Contains(t, content, "private_key_file: /etc/gameap-daemon/certs/server.key")
	assert.Contains(t, content, "work_path: /srv/gameap")
	assert.Contains(t, content, "log_level: info")
	assert.Contains(t, content, "enabled: true")
	assert.Contains(t, content, "address: panel.example.com:31718")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestWriteEnrollConfig_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "gameap-daemon.yaml")

	cfg := &EnrollConfig{
		NodeID:     1,
		ListenPort: 31717,
		GRPC: EnrollGRPC{
			Enabled: true,
			Address: "localhost:31718",
		},
	}

	err := WriteEnrollConfig(path, cfg)
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err)
}
