package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
