package config

import "errors"

var (
	ErrEmptyNodeID  = errors.New("empty node ID")
	ErrEmptyAPIHost = errors.New("empty API Host")
	ErrEmptyAPIKey  = errors.New("empty API Key")
)

type Scripts struct {
	Install     string
	Reinstall   string
	Update      string
	Start       string
	Pause       string
	Unpause     string
	Stop        string
	Kill        string
	Restart     string
	Status      string
	GetConsole  string
	SendCommand string
	Delete      string
}

type Config struct {
	NodeID uint `yaml:"ds_id"`

	ListenIP   string `yaml:"listen_ip"`
	ListenPort int    `yaml:"listen_port"`

	APIHost string `yaml:"api_host"`
	APIKey  string `yaml:"api_key"`

	DaemonLogin            string `yaml:"daemon_login"`
	DaemonPassword         string `yaml:"daemon_password"`
	PasswordAuthentication bool   `yaml:"password_authentication"`

	CACertificateFile    string `yaml:"ca_certificate_file"`
	CertificateChainFile string `yaml:"certificate_chain_file"`
	PrivateKeyFile       string `yaml:"private_key_file"`
	PrivateKeyPassword   string `yaml:"private_key_password"`
	DHFile               string `yaml:"dh_file"`

	IFList     []string `yaml:"if_list"`
	DrivesList []string `yaml:"drives_list"`

	StatsUpdatePeriod   int `yaml:"stats_update_period"`
	StatsDBUpdatePeriod int `yaml:"stats_db_update_period"`

	// Log config
	LogLevel  string `yaml:"log_level"`
	OutputLog string `yaml:"output_log"`
	ErrorLog  string `yaml:"error_log"`

	// Dedicated server config
	Path7zip    string `yaml:"path_7zip"`
	PathStarter string `yaml:"path_starter"`

	WorkPath     string `yaml:"work_path"`
	SteamCMDPath string `yaml:"steamcmd_path"`

	Scripts Scripts
}

func NewConfig() *Config {
	return &Config{
		ListenIP:   "0.0.0.0",
		ListenPort: 31717,

		LogLevel: "info",
	}
}

func (c *Config) Validate() error {
	if c.NodeID == 0 {
		return ErrEmptyNodeID
	}

	if c.APIHost == "" {
		return ErrEmptyAPIHost
	}

	if c.APIKey == "" {
		return ErrEmptyAPIKey
	}

	return nil
}
