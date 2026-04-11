package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServer_StartCommand_fallback_to_game_mod(t *testing.T) {
	tests := []struct {
		name           string
		startCommand   string
		gameModLinux   string
		gameModWindows string
		wantCommand    string
	}{
		{
			name:         "server_command_set_returns_server_command",
			startCommand: "./server.sh -game cstrike",
			gameModLinux: "./default.sh",
			wantCommand:  "./server.sh -game cstrike",
		},
		{
			name:         "empty_server_command_returns_game_mod_default",
			startCommand: "",
			gameModLinux: "./default.sh -game valve",
			wantCommand:  "./default.sh -game valve",
		},
		{
			name:         "empty_server_command_and_empty_game_mod_returns_empty",
			startCommand: "",
			gameModLinux: "",
			wantCommand:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(
				1,
				true,
				ServerInstalled,
				false,
				"test",
				"uuid-1",
				"uuid1234",
				Game{},
				GameMod{
					DefaultStartCMDLinux:   tt.gameModLinux,
					DefaultStartCMDWindows: tt.gameModWindows,
				},
				"127.0.0.1",
				27015,
				27016,
				27017,
				"password",
				"/srv/server",
				"gameap",
				tt.startCommand,
				"",
				"",
				"",
				false,
				time.Time{},
				nil,
				nil,
				time.Time{},
				0,
				0,
			)

			assert.Equal(t, tt.wantCommand, server.StartCommand())
		})
	}
}
