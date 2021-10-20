package config

type Config struct {
	DsId int

	ListenIP   string
	ListenPort int

	APIHost string
	APIKey  string

	DaemonLogin            string
	DaemonPassword         string
	PasswordAuthentication bool

	CACertificateFile    string
	CertificateChainFile string
	PrivateKeyFile       string
	PrivateKeyPassword   string
	DHFile               string

	IFList     []string
	DrivesList []string

	StatsUpdatePeriod   int
	StatsDbUpdatePeriod int

	// Log config
	LogLevel  string
	OutputLog string
	ErrorLog  string

	// Dedicated server config
	Path7zip    string
	PathStarter string

	WorkPath     string
	SteamCMDPath string

	ScriptInstall     string
	ScriptReinstall   string
	ScriptUpdate      string
	ScriptStart       string
	ScriptPause       string
	ScriptUnpause     string
	ScriptStop        string
	ScriptKill        string
	ScriptRestart     string
	ScriptStatus      string
	ScriptGetConsole  string
	ScriptSendCOmmand string
	ScriptDelete      string
}
