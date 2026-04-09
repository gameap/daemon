package config

import (
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/pkg/errors"
)

type EnrollConfig struct {
	NodeID               uint       `yaml:"ds_id"`
	APIKey               string     `yaml:"api_key"`
	ListenIP             string     `yaml:"listen_ip"`
	ListenPort           int        `yaml:"listen_port"`
	CACertificateFile    string     `yaml:"ca_certificate_file"`
	CertificateChainFile string     `yaml:"certificate_chain_file"`
	PrivateKeyFile       string     `yaml:"private_key_file"`
	WorkPath             string     `yaml:"work_path"`
	LogLevel             string     `yaml:"log_level"`
	GRPC                 EnrollGRPC `yaml:"grpc"`
}

type EnrollGRPC struct {
	Enabled bool   `yaml:"enabled"`
	Address string `yaml:"address"`
}

func WriteEnrollConfig(path string, cfg *EnrollConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.Wrap(err, "failed to marshal config")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrap(err, "failed to create config directory")
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return errors.Wrap(err, "failed to write config file")
	}

	return nil
}
