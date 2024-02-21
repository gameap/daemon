package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gameap/daemon/internal/processmanager"
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

type SteamConfig struct {
	Login    string `yaml:"login"`
	Password string `yaml:"password"`
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
	ToolsPath    string `yaml:"tools_path"`
	SteamCMDPath string `yaml:"steamcmd_path"`

	SteamConfig SteamConfig `yaml:"steam_config"`

	Scripts Scripts

	TaskManager struct {
		UpdatePeriod  time.Duration `yaml:"update_period"`
		RunTaskPeriod time.Duration `yaml:"run_task_period"`
		WorkersCount  int           `yaml:"workers_count"`
	} `yaml:"task_manager"`

	ProcessManager struct {
		Name   string            `yaml:"name"`
		Config map[string]string `yaml:"config"`
	} `yaml:"process_manager"`
}

func NewConfig() *Config {
	return &Config{
		ListenIP:   "0.0.0.0",
		ListenPort: 31717,

		LogLevel: "info",
	}
}

func (cfg *Config) Init() error {
	if cfg.ToolsPath == "" {
		cfg.ToolsPath = filepath.Join(cfg.WorkPath, "tools")
	}

	if cfg.TaskManager.UpdatePeriod == 0 {
		cfg.TaskManager.UpdatePeriod = 1 * time.Second
	}

	if cfg.TaskManager.RunTaskPeriod == 0 {
		cfg.TaskManager.RunTaskPeriod = 10 * time.Millisecond
	}

	cfg.ProcessManager.Name = processmanager.Default

	return cfg.validate()
}

func (cfg *Config) validate() error {
	if cfg.NodeID == 0 {
		return ErrEmptyNodeID
	}

	if cfg.APIHost == "" {
		return ErrEmptyAPIHost
	}

	if cfg.APIKey == "" {
		return ErrEmptyAPIKey
	}

	if _, err := os.Stat(cfg.CACertificateFile); err != nil {
		return NewInvalidFileError("invalid CA certificate file (ca_certificate_file)", err)
	}

	if _, err := os.Stat(cfg.CertificateChainFile); err != nil {
		return NewInvalidFileError("invalid certificate chain file (certificate_chain_file)", err)
	}

	if _, err := os.Stat(cfg.PrivateKeyFile); err != nil {
		return NewInvalidFileError("invalid private key file (private_key_file)", err)
	}

	return nil
}

func (cfg *Config) WorkDir() string {
	return cfg.WorkPath
}
