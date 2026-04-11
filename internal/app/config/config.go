package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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

type GRPCConfig struct {
	Enabled               bool          `yaml:"enabled"`
	Insecure              bool          `yaml:"insecure"`
	Address               string        `yaml:"address"`
	HeartbeatInterval     time.Duration `yaml:"heartbeat_interval"`
	ConnectTimeout        time.Duration `yaml:"connect_timeout"`
	InitialReconnectDelay time.Duration `yaml:"initial_reconnect_delay"`
	MaxReconnectDelay     time.Duration `yaml:"max_reconnect_delay"`
}

//nolint:govet
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
	CACertificate        string `yaml:"ca_certificate"`
	CertificateChainFile string `yaml:"certificate_chain_file"`
	CertificateChain     string `yaml:"certificate_chain"`
	PrivateKeyFile       string `yaml:"private_key_file"`
	PrivateKey           string `yaml:"private_key"`
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

	GRPC GRPCConfig `yaml:"grpc"`

	Users map[string]string `yaml:"users"`

	// Windows specific settings

	// If true, the daemon will run servers under the "NT AUTHORITY\NETWORK SERVICE" user.
	// This user has limited permissions and is suitable for running game servers securely.
	// If false, servers will run under the user specified in the "users" section of the config.
	UseNetworkServiceUser bool `yaml:"use_network_service_user"`
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

	if cfg.ProcessManager.Name == "" {
		cfg.ProcessManager.Name = detectDefaultProcessManager()
	}

	// GRPC defaults
	if cfg.GRPC.HeartbeatInterval == 0 {
		cfg.GRPC.HeartbeatInterval = 30 * time.Second
	}

	if cfg.GRPC.ConnectTimeout == 0 {
		cfg.GRPC.ConnectTimeout = 30 * time.Second
	}

	if cfg.GRPC.InitialReconnectDelay == 0 {
		cfg.GRPC.InitialReconnectDelay = 1 * time.Second
	}

	if cfg.GRPC.MaxReconnectDelay == 0 {
		cfg.GRPC.MaxReconnectDelay = 60 * time.Second
	}

	return cfg.validate()
}

func (cfg *Config) validate() error {
	if cfg.NodeID == 0 {
		return ErrEmptyNodeID
	}

	if !cfg.GRPC.Enabled {
		if cfg.APIHost == "" {
			return ErrEmptyAPIHost
		}

		if cfg.APIKey == "" {
			return ErrEmptyAPIKey
		}
	}

	if !cfg.IsInsecure() {
		if cfg.CACertificate == "" {
			if cfg.CACertificateFile == "" {
				return ErrNoCACertificate
			}
			if _, err := os.Stat(cfg.CACertificateFile); err != nil {
				return NewInvalidFileError("invalid CA certificate file (ca_certificate_file)", err)
			}
		}

		if cfg.CertificateChain == "" {
			if cfg.CertificateChainFile == "" {
				return ErrNoCertificateChain
			}
			if _, err := os.Stat(cfg.CertificateChainFile); err != nil {
				return NewInvalidFileError("invalid certificate chain file (certificate_chain_file)", err)
			}
		}

		if cfg.PrivateKey == "" {
			if cfg.PrivateKeyFile == "" {
				return ErrNoPrivateKey
			}
			if _, err := os.Stat(cfg.PrivateKeyFile); err != nil {
				return NewInvalidFileError("invalid private key file (private_key_file)", err)
			}
		}
	}

	return nil
}

func (cfg *Config) CACertificatePEM() ([]byte, error) {
	if cfg.CACertificate != "" {
		return []byte(cfg.CACertificate), nil
	}
	return os.ReadFile(cfg.CACertificateFile)
}

func (cfg *Config) CertificateChainPEM() ([]byte, error) {
	if cfg.CertificateChain != "" {
		return []byte(cfg.CertificateChain), nil
	}
	return os.ReadFile(cfg.CertificateChainFile)
}

func (cfg *Config) PrivateKeyPEM() ([]byte, error) {
	if cfg.PrivateKey != "" {
		return []byte(cfg.PrivateKey), nil
	}
	return os.ReadFile(cfg.PrivateKeyFile)
}

func (cfg *Config) WorkDir() string {
	return cfg.WorkPath
}

func (cfg *Config) IsInsecure() bool {
	return strings.HasPrefix(cfg.APIHost, "http://")
}

func (cfg *Config) GRPCAddress() string {
	if cfg.GRPC.Address != "" {
		return cfg.GRPC.Address
	}

	host := cfg.APIHost
	// Remove scheme (https://, http://)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	// Remove path if present
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}

	// Replace port with default gRPC port if present
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		host = host[:colonIdx]
	}
	return host + ":31718"
}

func UpdateEnvPath(cfg *Config) error {
	if cfg.ToolsPath == "" {
		return nil
	}

	currentPath := os.Getenv("PATH")
	pathSeparator := string(os.PathListSeparator)
	existingPaths := strings.Split(currentPath, pathSeparator)

	existingPathsSet := make(map[string]struct{}, len(existingPaths))
	for _, p := range existingPaths {
		existingPathsSet[p] = struct{}{}
	}

	var pathsToAdd []string

	if info, err := os.Stat(cfg.ToolsPath); err == nil && info.IsDir() {
		if _, exists := existingPathsSet[cfg.ToolsPath]; !exists {
			pathsToAdd = append(pathsToAdd, cfg.ToolsPath)
		}

		entries, err := os.ReadDir(cfg.ToolsPath)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					subdir := filepath.Join(cfg.ToolsPath, entry.Name())
					if _, exists := existingPathsSet[subdir]; !exists {
						pathsToAdd = append(pathsToAdd, subdir)
					}
				}
			}
		}
	}

	if len(pathsToAdd) == 0 {
		return nil
	}

	newPath := strings.Join(pathsToAdd, pathSeparator) + pathSeparator + currentPath
	return os.Setenv("PATH", newPath)
}
