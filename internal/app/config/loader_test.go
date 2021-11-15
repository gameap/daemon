package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpdatePaths(t *testing.T) {
	cfg := &Config{
		CACertificateFile:    "./certs/ca.crt",
		CertificateChainFile: "./certs/server.crt",
		PrivateKeyFile:       "./certs/server.key",
		DHFile:               "./certs/dh2048.pem",
	}

	updatedCfg := updatePaths("/etc/gameap-daemon/gameap-daemon.cfg", cfg)

	assert.Equal(t, "/etc/gameap-daemon/certs/ca.crt", updatedCfg.CACertificateFile)
	assert.Equal(t, "/etc/gameap-daemon/certs/server.crt", updatedCfg.CertificateChainFile)
	assert.Equal(t, "/etc/gameap-daemon/certs/server.key", updatedCfg.PrivateKeyFile)
	assert.Equal(t, "/etc/gameap-daemon/certs/dh2048.pem", updatedCfg.DHFile)
}
