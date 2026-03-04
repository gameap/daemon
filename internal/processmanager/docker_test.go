//go:build linux || darwin || windows

package processmanager

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDocker(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}

	pm := NewDocker(cfg, nil, nil)

	assert.NotNil(t, pm)
	assert.Equal(t, cfg, pm.cfg)
}

func TestDocker_containerName(t *testing.T) {
	cfg := &config.Config{
		WorkPath: "/tmp/test",
	}
	pm := NewDocker(cfg, nil, nil)

	tests := []struct {
		name           string
		server         *domain.Server
		expectedResult string
	}{
		{
			name: "uses docker_container_name from vars",
			server: createTestServer(map[string]string{
				"docker_container_name": "my-custom-container",
			}, nil, nil),
			expectedResult: "my-custom-container",
		},
		{
			name:           "uses UUID when no custom name",
			server:         createTestServer(nil, nil, nil),
			expectedResult: "test-uuid-1234",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pm.containerName(tt.server)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestDocker_getConfig_priority(t *testing.T) {
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
			key:            keyDockerImage,
			expectedResult: "server-image",
		},
		{
			name:           "gamemod metadata second priority",
			serverVars:     nil,
			gameModMeta:    map[string]any{"docker_image": "gamemod-image"},
			gameMeta:       map[string]any{"docker_image": "game-image"},
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyDockerImage,
			expectedResult: "gamemod-image",
		},
		{
			name:           "game metadata third priority",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       map[string]any{"docker_image": "game-image"},
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyDockerImage,
			expectedResult: "game-image",
		},
		{
			name:           "pm config lowest priority",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       nil,
			pmConfig:       map[string]string{"image": "pm-image"},
			key:            keyDockerImage,
			expectedResult: "pm-image",
		},
		{
			name:           "returns empty when not found",
			serverVars:     nil,
			gameModMeta:    nil,
			gameMeta:       nil,
			pmConfig:       nil,
			key:            keyDockerImage,
			expectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				WorkPath: "/tmp/test",
			}
			cfg.ProcessManager.Config = tt.pmConfig

			pm := NewDocker(cfg, nil, nil)
			server := createTestServer(tt.serverVars, tt.gameModMeta, tt.gameMeta)

			result := pm.getConfig(server, tt.key)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestNormalizeImageName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", defaultImage},
		{"nginx", "nginx:latest"},
		{"nginx:1.21", "nginx:1.21"},
		{"nginx@sha256:abc123", "nginx@sha256:abc123"},
		{"my-registry.com/image", "my-registry.com/image:latest"},
		{"my-registry.com/image:v1", "my-registry.com/image:v1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeImageName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseMemoryLimit(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"", 0, false},
		{"1024", 1024, false},
		{"1k", 1024, false},
		{"1K", 1024, false},
		{"1m", 1024 * 1024, false},
		{"1M", 1024 * 1024, false},
		{"1g", 1024 * 1024 * 1024, false},
		{"1G", 1024 * 1024 * 1024, false},
		{"2g", 2 * 1024 * 1024 * 1024, false},
		{"512m", 512 * 1024 * 1024, false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseMemoryLimit(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseCPULimit(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"", 0, false},
		{"1", 1e9, false},
		{"2", 2e9, false},
		{"0.5", int64(0.5e9), false},
		{"1.5", int64(1.5e9), false},
		{"invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseCPULimit(tt.input)
			if tt.hasError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseExtraVolumes(t *testing.T) {
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
			result := parseExtraVolumes(tt.input, "/tmp/workdir")
			assert.Len(t, result, tt.expected)
		})
	}
}

func createTestServer(vars map[string]string, gameModMeta, gameMeta map[string]any) *domain.Server {
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
		"test-uuid-1234",                      // uuid
		"test1234",                            // uuidShort
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
