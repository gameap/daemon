package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "sc_create_password",
			input:    `sc create gameapServer1 obj=gameap password=s3cret binPath=shawl.exe`,
			expected: `sc create gameapServer1 obj=gameap password=*** binPath=shawl.exe`,
		},
		{
			name:     "case_insensitive",
			input:    "PASSWORD=s3cret",
			expected: "PASSWORD=***",
		},
		{
			name:     "token_and_secret",
			input:    "--token=abc --secret=def",
			expected: "--token=*** --secret=***",
		},
		{
			name:     "api_key",
			input:    "api_key=abc apikey=def",
			expected: "api_key=*** apikey=***",
		},
		{
			name:     "nothing_to_redact",
			input:    "sc query gameapServer1",
			expected: "sc query gameapServer1",
		},
		{
			name:     "similar_word_is_left_alone",
			input:    "passwordfile=/etc/pw",
			expected: "passwordfile=/etc/pw",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, redactCommand(tt.input))
		})
	}
}

func TestRedactArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "assignment_argument",
			input:    []string{"sc", "create", "svc", "password=s3cret"},
			expected: []string{"sc", "create", "svc", "password=***"},
		},
		{
			name:     "value_follows_flag",
			input:    []string{"mysql", "-u", "root", "-p", "s3cret"},
			expected: []string{"mysql", "-u", "root", "-p", "***"},
		},
		{
			name:     "long_flag",
			input:    []string{"tool", "--password", "s3cret", "--verbose"},
			expected: []string{"tool", "--password", "***", "--verbose"},
		},
		{
			name:     "flag_at_end_has_no_value",
			input:    []string{"tool", "--password"},
			expected: []string{"tool", "--password"},
		},
		{
			name:     "nothing_to_redact",
			input:    []string{"sc", "query", "gameapServer1"},
			expected: []string{"sc", "query", "gameapServer1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, redactArgs(tt.input))
		})
	}
}
