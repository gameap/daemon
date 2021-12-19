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

	updatedCfg := updatePaths(configPath, cfg)

	assert.Equal(t, caCertificateFilePath, updatedCfg.CACertificateFile)
	assert.Equal(t, certificateChainFilePath, updatedCfg.CertificateChainFile)
	assert.Equal(t, privateKeyFilePath, updatedCfg.PrivateKeyFile)
	assert.Equal(t, dhFilePathPath, updatedCfg.DHFile)
}
