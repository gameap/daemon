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
	}

	updatedCfg := updatePaths(configPath, cfg)

	assert.Equal(t, caCertificateFilePath, updatedCfg.CACertificateFile)
	assert.Equal(t, certificateChainFilePath, updatedCfg.CertificateChainFile)
	assert.Equal(t, privateKeyFilePath, updatedCfg.PrivateKeyFile)
}

func TestUpdatePaths_EmptyFilePaths(t *testing.T) {
	cfg := &Config{}

	updatedCfg := updatePaths(configPath, cfg)

	assert.Empty(t, updatedCfg.CACertificateFile)
	assert.Empty(t, updatedCfg.CertificateChainFile)
	assert.Empty(t, updatedCfg.PrivateKeyFile)
}
