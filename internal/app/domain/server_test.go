package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
