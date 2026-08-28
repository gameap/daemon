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
			name:     "hyphenated_api_key_assignment",
			input:    "tool --api-key=abc",
			expected: "tool --api-key=***",
		},
		{
			name:     "hyphenated_api_key_separate_value",
			input:    "tool --api-key abc --verbose",
			expected: "tool --api-key *** --verbose",
		},
		{
			name:     "password_as_separate_value",
			input:    "mysql -u root --password s3cret",
			expected: "mysql -u root --password ***",
		},
		{
			name:     "short_password_flag_as_separate_value",
			input:    "mysql -u root -p s3cret",
			expected: "mysql -u root -p ***",
		},
		{
			name:     "sc_style_value_after_the_equals_sign",
			input:    "sc create svc password= s3cret",
			expected: "sc create svc password= ***",
		},
		{
			name:     "sc_style_quoted_value_after_the_equals_sign",
			input:    `sc create svc obj= gameap password= "my secret"`,
			expected: "sc create svc obj= gameap password= ***",
		},
		{
			name:     "equals_sign_at_end_of_line_is_left_alone",
			input:    "password=\nnext line",
			expected: "password=\nnext line",
		},
		{
			name:     "quoted_assignment_with_spaces",
			input:    `sc create svc password="my secret" binPath=shawl.exe`,
			expected: "sc create svc password=*** binPath=shawl.exe",
		},
		{
			name:     "quoted_separate_value_with_spaces",
			input:    `tool --token "a b c" --verbose`,
			expected: "tool --token *** --verbose",
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
			name:     "hyphenated_api_key_flag",
			input:    []string{"tool", "--api-key", "abc", "--verbose"},
			expected: []string{"tool", "--api-key", "***", "--verbose"},
		},
		{
			name:     "hyphenated_api_key_assignment",
			input:    []string{"tool", "--api-key=abc"},
			expected: []string{"tool", "--api-key=***"},
		},
		{
			name:     "sc_style_bare_key_takes_the_next_argument",
			input:    []string{"sc", "create", "svc", "password=", "s3cret", "binPath=", "shawl.exe"},
			expected: []string{"sc", "create", "svc", "password=", "***", "binPath=", "shawl.exe"},
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
