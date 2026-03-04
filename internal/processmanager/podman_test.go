//go:build linux || darwin

package processmanager

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

func TestNewPodman(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}

	pm := NewPodman(cfg, nil, nil)

	assert.NotNil(t, pm)
	assert.Equal(t, cfg, pm.cfg)
	assert.NotEmpty(t, pm.socketPath)
	assert.NotNil(t, pm.httpClient)
}

func TestPodman_containerName(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}
	pm := NewPodman(cfg, nil, nil)

	tests := []struct {
		name           string
		server         *domain.Server
		expectedResult string
	}{
		{
			name: "uses docker_container_name from vars",
			server: createPodmanTestServer(map[string]string{
				"docker_container_name": "my-custom-container",
			}, nil, nil),
			expectedResult: "my-custom-container",
		},
		{
			name:           "uses UUID when no custom name",
			server:         createPodmanTestServer(nil, nil, nil),
			expectedResult: "test-uuid-5678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.containerName(tt.server)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestPodman_getConfig_priority(t *testing.T) {
	tests := []struct {
		name           string
		serverVars     map[string]string
		gameModMeta    map[string]any
		gameMeta       map[string]any
		pmConfig       map[string]string
		key            string
		expectedResult string
	}{
		{
			name:           "server vars have highest priority",
			serverVars:     map[string]string{"docker_image": "server-image"},
			gameModMeta:    map[string]any{"docker_image": "gamemod-image"},
			gameMeta:       map[string]any{"docker_image": "game-image"},
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyPodmanImage,
			expectedResult: "server-image",
		},
		{
			name:           "gamemod metadata second priority",
			serverVars:     nil,
			gameModMeta:    map[string]any{"docker_image": "gamemod-image"},
			gameMeta:       map[string]any{"docker_image": "game-image"},
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyPodmanImage,
			expectedResult: "gamemod-image",
		},
		{
			name:           "game metadata third priority",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       map[string]any{"docker_image": "game-image"},
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyPodmanImage,
			expectedResult: "game-image",
		},
		{
			name:           "pm config lowest priority",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       nil,
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyPodmanImage,
			expectedResult: "pm-image",
		},
		{
			name:           "returns empty when not found",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       nil,
			pmConfig:       nil,
			key:            keyPodmanImage,
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				WorkPath: "/tmp/test",
			}
			cfg.ProcessManager.Config = tt.pmConfig

			pm := NewPodman(cfg, nil, nil)
			server := createPodmanTestServer(tt.serverVars, tt.gameModMeta, tt.gameMeta)

			result := pm.getConfig(server, tt.key)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestPodman_buildPortMappings(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}
	pm := NewPodman(cfg, nil, nil)

	server := createPodmanTestServer(nil, nil, nil)
	portMappings := pm.buildPortMappings(server)

	// Should have: connect TCP, connect UDP, query UDP, rcon TCP
	assert.GreaterOrEqual(t, len(portMappings), 2) // At least connect port TCP+UDP

	// Check connect port is mapped
	found := false
	for _, pm := range portMappings {
		if pm["container_port"] == 27015 {
			found = true
			break
		}
	}
	assert.True(t, found, "connect port should be mapped")
}

func TestPodman_parseExtraVolumes(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}
	pm := NewPodman(cfg, nil, nil)

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "json array",
			input:    `["/data:/data", "/logs:/logs:ro"]`,
			expected: 2,
		},
		{
			name:     "comma-separated",
			input:    "/data:/data,/logs:/logs:ro",
			expected: 2,
		},
		{
			name:     "empty",
			input:    "",
			expected: 0,
		},
		{
			name:     "invalid format",
			input:    "invalid",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.parseExtraVolumes(tt.input, "/tmp/workdir")
			assert.Len(t, result, tt.expected)
		})
	}
}

func TestIsPodmanNotFoundError(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{assert.AnError, false},
		{&podmanTestError{msg: "container not found"}, true},
		{&podmanTestError{msg: "no such container"}, true},
		{&podmanTestError{msg: "no container with"}, true},
		{&podmanTestError{msg: "other error"}, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := isPodmanNotFoundError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetDefaultPodmanSocket(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected string
	}{
		{
			name: "uses socket_path from config",
			cfg: &config.Config{
				ProcessManager: struct {
					Name   string            `yaml:"name"`
					Config map[string]string `yaml:"config"`
				}{
					Config: map[string]string{
						"socket_path": "unix:///custom/podman.sock",
					},
				},
			},
			expected: "unix:///custom/podman.sock",
		},
		{
			name:     "falls back to default",
			cfg:      &config.Config{},
			expected: "unix:///run/podman/podman.sock", // or rootless socket depending on UID
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getDefaultPodmanSocket(tt.cfg)
			if tt.cfg.ProcessManager.Config != nil && tt.cfg.ProcessManager.Config["socket_path"] != "" {
				assert.Equal(t, tt.expected, result)
			} else {
				// For default case, just check it starts with unix://
				assert.Contains(t, result, "unix://")
			}
		})
	}
}

type podmanTestError struct {
	msg string
}

func (e *podmanTestError) Error() string {
	return e.msg
}

func createPodmanTestServer(vars map[string]string, gameModMeta, gameMeta map[string]any) *domain.Server {
	if vars == nil {
		vars = make(map[string]string)
	}
	if gameModMeta == nil {
		gameModMeta = make(map[string]any)
	}
	if gameMeta == nil {
		gameMeta = make(map[string]any)
	}

	return domain.NewServer(
		1,                                     // id
		true,                                  // enabled
		domain.ServerInstalled,                // installStatus
		false,                                 // blocked
		"Test Server",                         // name
		"test-uuid-5678",                      // uuid
		"test5678",                            // uuidShort
		domain.Game{Metadata: gameMeta},       // game
		domain.GameMod{Metadata: gameModMeta}, // gameMod
		"127.0.0.1",                           // ip
		27015,                                 // connectPort
		27016,                                 // queryPort
		27017,                                 // rconPort
		"password",                            // rconPassword
		"/servers/test",                       // dir
		"",                                    // user
		"./game_server -port {port}",          // startCommand
		"",                                    // stopCommand
		"",                                    // forceStopCommand
		"",                                    // restartCommand
		false,                                 // processActive
		time.Time{},                           // lastProcessCheck
		vars,                                  // vars
		domain.Settings{},                     // settings
		time.Time{},                           // updatedAt
	)
}
