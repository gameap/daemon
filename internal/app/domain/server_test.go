package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestServerForVars(
	gameModVars []GameModVarTemplate,
	vars map[string]string,
	settings Settings,
) *Server {
	return NewServer(
		1,
		true,
		ServerInstalled,
		false,
		"test",
		"test-uuid",
		"test",
		Game{StartCode: "game"},
		GameMod{Vars: gameModVars},
		"127.0.0.1",
		27015,
		27016,
		27017,
		"rconpass",
		"server-dir",
		"",
		"",
		"",
		"",
		"",
		false,
		time.Time{},
		vars,
		settings,
		time.Time{},
		0,
		0,
	)
}

func TestNormalizeEnvKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "some-value",
			expected: "SOME_VALUE",
		},
		{
			name:     "leading and trailing spaces",
			input:    " some variable with spaces ",
			expected: "SOME_VARIABLE_WITH_SPACES",
		},
		{
			name:     "special characters removed",
			input:    "some var with $%#",
			expected: "SOME_VAR_WITH",
		},
		{
			name:     "already uppercase",
			input:    "SERVER_PORT",
			expected: "SERVER_PORT",
		},
		{
			name:     "mixed case with dashes",
			input:    "Max-Players",
			expected: "MAX_PLAYERS",
		},
		{
			name:     "numbers preserved",
			input:    "value123test",
			expected: "VALUE123TEST",
		},
		{
			name:     "multiple dashes",
			input:    "some--value",
			expected: "SOME__VALUE",
		},
		{
			name:     "multiple spaces",
			input:    "some  value",
			expected: "SOME__VALUE",
		},
		{
			name:     "underscores preserved",
			input:    "some_value",
			expected: "SOME_VALUE",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "$%#@!",
			expected: "",
		},
		{
			name:     "leading underscore after normalization trimmed",
			input:    "-value",
			expected: "VALUE",
		},
		{
			name:     "trailing underscore after normalization trimmed",
			input:    "value-",
			expected: "VALUE",
		},
		{
			name:     "unicode letters preserved and uppercased",
			input:    "тест value",
			expected: "ТЕСТ_VALUE",
		},
		{
			name:     "mixed special and valid",
			input:    "!@#test$%^value&*()",
			expected: "TESTVALUE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeEnvKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestServer_Vars_ReturnsGameModDefaults(t *testing.T) {
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
			{Key: "maxplayers", DefaultValue: "16"},
		},
		map[string]string{},
		Settings{},
	)

	vars := server.Vars()

	assert.Equal(t, "Default Hostname", vars["hostname"])
	assert.Equal(t, "16", vars["maxplayers"])
}

func TestServer_Vars_ServerVarsOverrideGameModDefaults(t *testing.T) {
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
			{Key: "maxplayers", DefaultValue: "16"},
		},
		map[string]string{
			"hostname": "Vars Hostname",
		},
		Settings{},
	)

	vars := server.Vars()

	assert.Equal(t, "Vars Hostname", vars["hostname"])
	assert.Equal(t, "16", vars["maxplayers"])
}

func TestServer_Vars_SettingsOverrideVarsAndGameModDefaults(t *testing.T) {
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
			{Key: "maxplayers", DefaultValue: "16"},
		},
		map[string]string{
			"hostname":   "Vars Hostname",
			"maxplayers": "32",
		},
		Settings{
			"hostname": "Settings Hostname",
		},
	)

	vars := server.Vars()

	assert.Equal(t, "Settings Hostname", vars["hostname"])
	assert.Equal(t, "32", vars["maxplayers"])
}

func TestServer_Vars_EmptyWhenNoSources(t *testing.T) {
	server := newTestServerForVars(nil, map[string]string{}, Settings{})

	vars := server.Vars()

	assert.Empty(t, vars)
}

func TestServer_EnvironmentVars_NormalizesKeys(t *testing.T) {
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "max-players", DefaultValue: "16"},
		},
		map[string]string{},
		Settings{},
	)

	envVars := server.EnvironmentVars()

	assert.Equal(t, "16", envVars["MAX_PLAYERS"])
}

func TestServer_EnvironmentVars_SettingsOverrideVarsAndGameModDefaults(t *testing.T) {
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
			{Key: "maxplayers", DefaultValue: "16"},
		},
		map[string]string{
			"hostname":   "Vars Hostname",
			"maxplayers": "32",
		},
		Settings{
			"hostname": "Settings Hostname",
		},
	)

	envVars := server.EnvironmentVars()

	assert.Equal(t, "Settings Hostname", envVars["HOSTNAME"])
	assert.Equal(t, "32", envVars["MAXPLAYERS"])
}

func TestServer_EnvironmentVars_AlwaysIncludesPortVars(t *testing.T) {
	server := newTestServerForVars(nil, map[string]string{}, Settings{})

	envVars := server.EnvironmentVars()

	assert.Equal(t, "27015", envVars["SERVER_PORT"])
	assert.Equal(t, "27015", envVars["PORT"])
	assert.Equal(t, "27016", envVars["QUERY_PORT"])
	assert.Equal(t, "27017", envVars["RCON_PORT"])
}
