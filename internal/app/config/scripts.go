package config

func InitDefaultScripts(cfg *Config) {
	if cfg.Scripts.Start == "" {
		cfg.Scripts.Start = DefaultGameServerScriptStart
	}

	if cfg.Scripts.Stop == "" {
		cfg.Scripts.Stop = DefaultGameServerScriptStop
	}

	if cfg.Scripts.Restart == "" {
		cfg.Scripts.Restart = DefaultGameServerScriptRestart
	}

	if cfg.Scripts.Status == "" {
		cfg.Scripts.Status = DefaultGameServerScriptStatus
	}

	if cfg.Scripts.GetConsole == "" {
		cfg.Scripts.GetConsole = DefaultGameServerScriptGetOutput
	}

	if cfg.Scripts.SendCommand == "" {
		cfg.Scripts.SendCommand = DefaultGameServerScriptSendInput
	}
}
