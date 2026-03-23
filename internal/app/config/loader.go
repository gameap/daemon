package config

import (
	"bufio"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"github.com/goccy/go-yaml"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

func Load(path string) (*Config, error) {
	var err error
	var cfg *Config

	if path == "" {
		path = findConfigFile()
	}

	if path == "" {
		return nil, ErrConfigNotFound
	}

	ext := filepath.Ext(path)

	switch ext {
	case ".yaml", ".yml":
		cfg, err = loadYaml(path)
	}
	if err != nil {
		return nil, errors.WithMessage(err, "load config file")
	}
	if cfg == nil {
		return nil, ErrUnsupportedConfigFormat
	}

	cfg = updatePaths(path, cfg)

	err = cfg.Init()
	if err != nil {
		return nil, err
	}

	return cfg, err
}

func loadYaml(path string) (*Config, error) {
	cfg := NewConfig()

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(bufio.NewReader(file))
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(bytes, cfg)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func updatePaths(cfgPath string, cfg *Config) *Config {
	if cfg == nil {
		return nil
	}

	if !filepath.IsAbs(cfgPath) {
		cfgPath, _ = filepath.Abs(cfgPath)
	}

	cfgDirPath := filepath.Dir(cfgPath)

	if cfg.CACertificateFile != "" && !filepath.IsAbs(cfg.CACertificateFile) {
		cfg.CACertificateFile, _ = filepath.Abs(filepath.Join(cfgDirPath, cfg.CACertificateFile))
	}

	if cfg.CertificateChainFile != "" && !filepath.IsAbs(cfg.CertificateChainFile) {
		cfg.CertificateChainFile, _ = filepath.Abs(filepath.Join(cfgDirPath, cfg.CertificateChainFile))
	}

	if cfg.PrivateKeyFile != "" && !filepath.IsAbs(cfg.PrivateKeyFile) {
		cfg.PrivateKeyFile, _ = filepath.Abs(filepath.Join(cfgDirPath, cfg.PrivateKeyFile))
	}

	if cfg.DHFile != "" && !filepath.IsAbs(cfg.DHFile) {
		cfg.DHFile, _ = filepath.Abs(filepath.Join(cfgDirPath, cfg.DHFile))
	}

	return cfg
}

func findConfigFile() string {
	cfgPaths := []string{
		"./gameap-daemon.cfg",
		"./gameap-daemon.yml",
		"./gameap-daemon.yaml",
		"/etc/gameap-daemon/gameap-daemon.cfg",
		"/etc/gameap-daemon/gameap-daemon.yml",
		"/etc/gameap-daemon/gameap-daemon.yaml",
		"/etc/gameap-daemon/daemon.cfg",
		"/etc/gameap-daemon/daemon.yml",
		"/etc/gameap-daemon/daemon.yaml",
		"/etc/gameap/daemon.cfg",
		"/etc/gameap/daemon.yaml",
		"/etc/gameap/daemon.yml",
		"/etc/gameap/gameap-daemon.cfg",
		"/etc/gameap/gameap-daemon.yaml",
		"/etc/gameap/gameap-daemon.yml",
		"/etc/gameap-daemon.cfg",
		"/etc/gameap-daemon.yml",
		"/etc/gameap-daemon.yaml",
	}

	systemUser, err := user.Current()
	if err == nil {
		cfgPaths = append(cfgPaths, filepath.Join(systemUser.HomeDir, "gameap-daemon.cfg"))
		cfgPaths = append(cfgPaths, filepath.Join(systemUser.HomeDir, "gameap-daemon.yml"))
		cfgPaths = append(cfgPaths, filepath.Join(systemUser.HomeDir, "gameap-daemon.yaml"))
	}

	log.Info("Looking up configuration file")

	for _, path := range cfgPaths {
		if _, err = os.Stat(path); err == nil {
			log.Infof("Found config file: %s", path)
			return path
		}
	}

	return ""
}
