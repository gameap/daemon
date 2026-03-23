package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_ToolsPathNotSet_InitializedDefaultToolsPath(t *testing.T) {
	cfg := givenValidConfig(t)

	err := cfg.Init()

	assert.Nil(t, err)
	assert.Equal(t, "/tmp/config_test/tools", cfg.ToolsPath)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name            string
		configSetupFunc func(cfg *Config)
		expectedError   error
	}{
		{
			"empty NodeID",
			func(cfg *Config) {
				cfg.NodeID = 0
			},
			ErrEmptyNodeID,
		},
		{
			"empty APIHost",
			func(cfg *Config) {
				cfg.APIHost = ""
			},
			ErrEmptyAPIHost,
		},
		{
			"empty APIKey",
			func(cfg *Config) {
				cfg.APIKey = ""
			},
			ErrEmptyAPIKey,
		},
		{
			"no CA certificate source",
			func(cfg *Config) {
				cfg.CACertificateFile = ""
				cfg.CACertificate = ""
			},
			ErrNoCACertificate,
		},
		{
			"no certificate chain source",
			func(cfg *Config) {
				cfg.CertificateChainFile = ""
				cfg.CertificateChain = ""
			},
			ErrNoCertificateChain,
		},
		{
			"no private key source",
			func(cfg *Config) {
				cfg.PrivateKeyFile = ""
				cfg.PrivateKey = ""
			},
			ErrNoPrivateKey,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := givenValidConfig(t)
			test.configSetupFunc(cfg)

			err := cfg.Init()

			assert.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestValidate_InlineCerts(t *testing.T) {
	cfg := NewConfig()
	cfg.NodeID = 1
	cfg.APIHost = "http://localhost"
	cfg.APIKey = "api-key"
	cfg.WorkPath = "/tmp/config_test"
	cfg.CACertificate = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"
	cfg.CertificateChain = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"
	cfg.PrivateKey = "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"

	err := cfg.Init()

	assert.Nil(t, err)
}

func TestCACertificatePEM_Inline(t *testing.T) {
	cfg := &Config{CACertificate: "inline-ca-pem"}

	pem, err := cfg.CACertificatePEM()

	require.NoError(t, err)
	assert.Equal(t, []byte("inline-ca-pem"), pem)
}

func TestCACertificatePEM_File(t *testing.T) {
	cfg := &Config{CACertificateFile: "../../../config/certs/rootca.crt"}

	pem, err := cfg.CACertificatePEM()

	require.NoError(t, err)
	expected, _ := os.ReadFile("../../../config/certs/rootca.crt")
	assert.Equal(t, expected, pem)
}

func TestCACertificatePEM_InlineTakesPrecedence(t *testing.T) {
	cfg := &Config{
		CACertificate:     "inline-value",
		CACertificateFile: "../../../config/certs/rootca.crt",
	}

	pem, err := cfg.CACertificatePEM()

	require.NoError(t, err)
	assert.Equal(t, []byte("inline-value"), pem)
}

func TestCertificateChainPEM_Inline(t *testing.T) {
	cfg := &Config{CertificateChain: "inline-cert-pem"}

	pem, err := cfg.CertificateChainPEM()

	require.NoError(t, err)
	assert.Equal(t, []byte("inline-cert-pem"), pem)
}

func TestPrivateKeyPEM_Inline(t *testing.T) {
	cfg := &Config{PrivateKey: "inline-key-pem"}

	pem, err := cfg.PrivateKeyPEM()

	require.NoError(t, err)
	assert.Equal(t, []byte("inline-key-pem"), pem)
}

func givenValidConfig(t *testing.T) *Config {
	t.Helper()

	cfg := NewConfig()
	cfg.NodeID = 1
	cfg.APIHost = "http://localhost"
	cfg.APIKey = "api-key"
	cfg.WorkPath = "/tmp/config_test"
	cfg.CACertificateFile = "../../../config/certs/rootca.crt"
	cfg.CertificateChainFile = "../../../config/certs/server.crt"
	cfg.PrivateKeyFile = "../../../config/certs/server.key"

	return cfg
}
