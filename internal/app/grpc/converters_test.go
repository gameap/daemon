package grpc

import (
	"testing"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
)

func TestProtoGameModToDomain_KeepsVarDefaultsVerbatim(t *testing.T) {
	// The panel canonicalizes every variable before it is sent: a bool is its
	// true_value/false_value text, a number is its decimal text. Nothing of it
	// may be reinterpreted here.
	protoVars := []*pb.GameModVar{
		{Var: "ase", Default: "1", Info: "All Seeing Eye", AdminVar: true},
		{Var: "pvp", Default: "0"},
		{Var: "debug", Default: "true"},
		{Var: "hardcore", Default: "false"},
		{Var: "code", Default: "007"},
		{Var: "ratio", Default: "1.50"},
		{Var: "password", Default: ""},
		{Var: "game", Default: "baseq2"},
		{Var: "SERVER_TOKEN", Default: "abc"},
	}

	gameMod := ProtoGameModToDomain(&pb.GameMod{Id: 10, GameCode: "q2", Name: "Default", Vars: protoVars})

	expected := make([]domain.GameModVarTemplate, 0, len(protoVars))
	for _, v := range protoVars {
		expected = append(expected, domain.GameModVarTemplate{Key: v.Var, DefaultValue: v.Default})
	}

	assert.Equal(t, expected, gameMod.Vars)
}

func TestParseProtoSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings []*pb.ServerSetting
		expected domain.Settings
	}{
		{
			name:     "nil_slice_means_no_settings",
			settings: nil,
			expected: nil,
		},
		{
			name:     "empty_slice_means_no_settings",
			settings: []*pb.ServerSetting{},
			expected: nil,
		},
		{
			name: "values_are_kept_verbatim",
			settings: []*pb.ServerSetting{
				{Name: "ase", Value: "0"},
				{Name: "code", Value: "007"},
				{Name: "ratio", Value: "1.50"},
				{Name: "password", Value: ""},
				{Name: "autostart", Value: "true"},
			},
			expected: domain.Settings{
				"ase":       "0",
				"code":      "007",
				"ratio":     "1.50",
				"password":  "",
				"autostart": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseProtoSettings(tt.settings))
		})
	}
}

func TestParseVarsJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty_input",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "values_are_kept_verbatim",
			input:    `{"map":"q2dm1","code":"007","empty":""}`,
			expected: map[string]string{"map": "q2dm1", "code": "007", "empty": ""},
		},
		{
			name:     "invalid_json_yields_no_vars",
			input:    `{"map":`,
			expected: map[string]string{},
		},
		{
			name:     "non_string_value_yields_no_vars",
			input:    `{"maxplayers": 32}`,
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseVarsJSON(tt.input))
		})
	}
}
