package config

import (
	"bufio"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	ext := filepath.Ext(path)
	var err error
	var cfg *Config

	switch ext {
	case ".yaml", ".yml":
		cfg, err = loadYaml(path)
	case ".cfg", ".ini":
		cfg, err = loadIni(path)
	}

	cfg = updatePaths(path, cfg)

	if err != nil {
		return nil, err
	}

	err = cfg.Validate()
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

func loadIni(path string) (*Config, error) {
	c, err := ini.Load(path)
	if err != nil {
		return nil, err
	}

	cfg := NewConfig()

	cfg.NodeID, err = c.Section("").Key("ds_id").Uint()
	if err != nil {
		return nil, err
	}

	cfg.ListenIP = c.Section("").Key("listen_ip").String()

	cfg.ListenPort = c.Section("").Key("listen_port").MustInt(31717)
	cfg.APIHost = c.Section("").Key("api_host").String()
	cfg.APIKey = c.Section("").Key("api_key").String()

	cfg.DaemonLogin = c.Section("").Key("daemon_login").String()
	cfg.DaemonPassword = c.Section("").Key("daemon_password").String()
	cfg.PasswordAuthentication = c.Section("").Key("password_authentication").MustBool(false)

	cfg.CACertificateFile = c.Section("").Key("ca_certificate_file").String()
	cfg.CertificateChainFile = c.Section("").Key("certificate_chain_file").String()
	cfg.PrivateKeyFile = c.Section("").Key("private_key_file").String()
	cfg.PrivateKeyPassword = c.Section("").Key("private_key_password").String()
	cfg.DHFile = c.Section("").Key("dh_file").String()

	cfg.LogLevel = c.Section("").Key("log_level").MustString("debug")

	cfg.Path7zip = c.Section("").
		Key("7zip_path").
		MustString("C:\\gameap\\tools\\7zip\\7za.exe")
	cfg.PathStarter = c.Section("").
		Key("starter_path").
		MustString("C:\\gameap\\daemon\\gameap-starter.exe")

	cfg.IFList = c.Section("").
		Key("if_list").
		Strings(" ")

	cfg.DrivesList = c.Section("").
		Key("drives_list").
		Strings(" ")

	return cfg, nil
}

func updatePaths(cfgPath string, cfg *Config) *Config {
	if !filepath.IsAbs(cfgPath) {
		cfgPath, _ = filepath.Abs(cfgPath)
	}

	cfgDirPath := filepath.Dir(cfgPath)

	if !filepath.IsAbs(cfg.CACertificateFile) {
		cfg.CACertificateFile, _ = filepath.Abs(filepath.Clean(cfgDirPath + "/" + cfg.CACertificateFile))
	}

	if !filepath.IsAbs(cfg.CertificateChainFile) {
		cfg.CertificateChainFile, _ = filepath.Abs(filepath.Clean(cfgDirPath + "/" + cfg.CertificateChainFile))
	}

	if !filepath.IsAbs(cfg.PrivateKeyFile) {
		cfg.PrivateKeyFile, _ = filepath.Abs(filepath.Clean(cfgDirPath + "/" + cfg.PrivateKeyFile))
	}

	if !filepath.IsAbs(cfg.DHFile) {
		cfg.DHFile, _ = filepath.Abs(filepath.Clean(cfgDirPath + "/" + cfg.DHFile))
	}

	return cfg
}
