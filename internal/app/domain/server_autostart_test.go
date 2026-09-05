package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServerForAutostart(enabled, blocked bool, settings Settings) *Server {
	return NewServer(
		1,
		enabled,
		ServerInstalled,
		blocked,
		"test",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		Game{StartCode: "cstrike"},
		GameMod{Name: "public"},
		"1.3.3.7",
		27015,
		27016,
		27017,
		"rcon",
		"server",
		"gameap-user",
		"./start.sh",
		"./stop.sh",
		"",
		"",
		false,
		time.Unix(0, 0),
		map[string]string{},
		settings,
		time.Now(),
		0,
		0,
	)
}

func TestServer_AutoStart(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		blocked  bool
		settings Settings
		want     bool
		wantCan  bool
	}{
		{
			name:     "no settings at all",
			enabled:  true,
			settings: Settings{},
			want:     false,
			wantCan:  false,
		},
		{
			name:     "nil settings map",
			enabled:  true,
			settings: nil,
			want:     false,
			wantCan:  false,
		},
		{
			name:     "autostart one",
			enabled:  true,
			settings: Settings{autostartSettingKey: "1"},
			want:     true,
			wantCan:  true,
		},
		{
			name:     "autostart true",
			enabled:  true,
			settings: Settings{autostartSettingKey: "true"},
			want:     true,
			wantCan:  true,
		},
		{
			name:     "autostart yes is case insensitive",
			enabled:  true,
			settings: Settings{autostartSettingKey: "YES"},
			want:     true,
			wantCan:  true,
		},
		{
			name:     "autostart false",
			enabled:  true,
			settings: Settings{autostartSettingKey: "false"},
			want:     false,
			wantCan:  false,
		},
		{
			name:     "autostart zero",
			enabled:  true,
			settings: Settings{autostartSettingKey: "0"},
			want:     false,
			wantCan:  false,
		},
		{
			name:    "current state wins over the persistent setting",
			enabled: true,
			settings: Settings{
				autostartSettingKey:        "1",
				autostartCurrentSettingKey: "0",
			},
			want:    false,
			wantCan: false,
		},
		{
			name:    "current state can enable autostart on its own",
			enabled: true,
			settings: Settings{
				autostartSettingKey:        "0",
				autostartCurrentSettingKey: "1",
			},
			want:    true,
			wantCan: true,
		},
		{
			name:    "empty current state falls back to the persistent setting",
			enabled: true,
			settings: Settings{
				autostartSettingKey:        "1",
				autostartCurrentSettingKey: "",
			},
			want:    true,
			wantCan: true,
		},
		{
			name:     "disabled server never autostarts",
			enabled:  false,
			settings: Settings{autostartSettingKey: "1"},
			want:     false,
			wantCan:  false,
		},
		{
			name:     "blocked server keeps the setting but must not be started",
			enabled:  true,
			blocked:  true,
			settings: Settings{autostartSettingKey: "1"},
			want:     true,
			wantCan:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServerForAutostart(tt.enabled, tt.blocked, tt.settings)

			assert.Equal(t, tt.want, server.AutoStart())
			assert.Equal(t, tt.wantCan, server.CanAutoStart())
		})
	}
}

// The process managers that supervise restarts themselves read the persistent
// setting, never AutoStart: autostart_current is 0 for the whole duration of a
// deliberate stop, and reading it would strip supervision from the unit.
func TestServer_AutoStartSetting_IgnoresCurrentState(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{
		autostartSettingKey:        "1",
		autostartCurrentSettingKey: "0",
	})

	assert.False(t, server.AutoStart())
	assert.True(t, server.AutoStartSetting())
}

func TestServer_AffectStop_SuppressesAutoStart(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "1"})
	require.True(t, server.AutoStart())

	server.AffectStop()

	assert.False(t, server.AutoStart(), "a deliberate stop must not be undone by the servers loop")
	assert.True(t, server.AutoStartSetting(), "the persistent preference is untouched")
}

func TestServer_AffectStart_RestoresAutoStart(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "1"})
	server.AffectStop()
	require.False(t, server.AutoStart())

	server.AffectStart()

	assert.True(t, server.AutoStart())
}

func TestServer_AffectStart_DoesNothingWhenAutostartIsOff(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "0"})

	server.AffectStart()

	assert.False(t, server.AutoStart())
	assert.Equal(t, "", server.Setting(autostartCurrentSettingKey))
}

// The panel can deliver a server with no settings at all, which leaves the map
// nil. Writing the current state into it must not panic.
func TestServer_AffectStart_WithNilSettings(t *testing.T) {
	server := newTestServerForAutostart(true, false, nil)

	assert.NotPanics(t, func() {
		server.AffectStart()
		server.AffectStop()
		server.SetSetting(autostartSettingKey, "1")
	})

	assert.True(t, server.AutoStartSetting())
}

func TestServer_SetStatusAt_TracksUptime(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{})
	start := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	assert.True(t, server.RunningSince().IsZero())

	server.SetStatusAt(start, true)
	assert.Equal(t, start, server.RunningSince())

	server.SetStatusAt(start.Add(time.Minute), true)
	assert.Equal(t, start, server.RunningSince(), "an uninterrupted run keeps its start time")

	server.SetStatusAt(start.Add(2*time.Minute), false)
	assert.True(t, server.RunningSince().IsZero())
	assert.Equal(t, start.Add(2*time.Minute), server.LastStatusCheck())

	server.SetStatusAt(start.Add(3*time.Minute), true)
	assert.Equal(t, start.Add(3*time.Minute), server.RunningSince(), "a new run starts a new clock")
}

func TestServer_StartAttempts(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{})
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, 0, server.StartAttempts())
	assert.True(t, server.NextStartAllowedAt().IsZero())

	server.NoticeStartAttempt(now, 5*time.Second)
	assert.Equal(t, 1, server.StartAttempts())
	assert.Equal(t, now.Add(5*time.Second), server.NextStartAllowedAt())

	server.NoticeStartAttempt(now, 15*time.Second)
	assert.Equal(t, 2, server.StartAttempts())

	server.ResetStartAttempts()
	assert.Equal(t, 0, server.StartAttempts())
	assert.True(t, server.NextStartAllowedAt().IsZero())
}

// A settings push from the panel replaces the whole map. The daemon's own
// autostart_current has to survive it, otherwise a stop the daemon performed on
// its own — a scheduled task, which the panel never records — is reverted and
// the servers loop starts the server back up.
func TestServer_Set_KeepsDaemonOwnedAutostartCurrent(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "1"})

	server.AffectStop()
	require.False(t, server.AutoStart())

	pushServerSettings(server, Settings{
		autostartSettingKey:        "1",
		autostartCurrentSettingKey: "1",
	})

	assert.False(t, server.AutoStart(), "a stale pushed value must not undo the daemon's own stop")
}

func TestServer_Set_TakesPanelSettingsWhenDaemonHasNoOpinion(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "0"})

	pushServerSettings(server, Settings{
		autostartSettingKey:        "1",
		autostartCurrentSettingKey: "1",
	})

	assert.True(t, server.AutoStart())
}

// The two sides converge: a panel start arrives as a task, the daemon runs it
// and sets its own value, so the next push is no longer overridden by a stale
// local one.
func TestServer_Set_ConvergesAfterTheDaemonActsAgain(t *testing.T) {
	server := newTestServerForAutostart(true, false, Settings{autostartSettingKey: "1"})

	server.AffectStop()
	server.AffectStart()

	pushServerSettings(server, Settings{
		autostartSettingKey:        "1",
		autostartCurrentSettingKey: "1",
	})

	assert.True(t, server.AutoStart())
}

func pushServerSettings(server *Server, settings Settings) {
	server.Set(
		true,
		ServerInstalled,
		false,
		"test",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		Game{StartCode: "cstrike"},
		GameMod{Name: "public"},
		"1.3.3.7",
		27015,
		27016,
		27017,
		"rcon",
		"server",
		"gameap-user",
		"./start.sh",
		"./stop.sh",
		"",
		"",
		false,
		time.Unix(0, 0),
		map[string]string{},
		settings,
		time.Now(),
		0,
		0,
	)
}
