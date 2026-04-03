package processmanager

import (
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
)

// getContainerConfig retrieves configuration value with priority:
// 1. Server vars
// 2. GameMod metadata
// 3. Game metadata
// 4. ProcessManager config
func getContainerConfig(cfg *config.Config, server *domain.Server, key string) string {
	// 1. Check server vars
	if val, ok := server.Vars()[key]; ok && val != "" {
		return val
	}

	// 2. Check game mod metadata
	if val, ok := server.GameMod().Metadata[key]; ok {
		if strVal, isStr := val.(string); isStr && strVal != "" {
			return strVal
		}
	}

	// 3. Check game metadata
	if val, ok := server.Game().Metadata[key]; ok {
		if strVal, isStr := val.(string); isStr && strVal != "" {
			return strVal
		}
	}

	// 4. Check process manager config
	if cfg.ProcessManager.Config != nil {
		// Map docker_ prefixed keys to config keys without prefix
		configKey := strings.TrimPrefix(key, "docker_")
		if val, ok := cfg.ProcessManager.Config[configKey]; ok && val != "" {
			return val
		}
		// Also check with original key
		if val, ok := cfg.ProcessManager.Config[key]; ok && val != "" {
			return val
		}
	}

	return ""
}
