package shellquote

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple command",
			input:    "echo hello",
			expected: []string{"echo", "hello"},
		},
		{
			name:     "command with multiple arguments",
			input:    "ls -la /tmp",
			expected: []string{"ls", "-la", "/tmp"},
		},
		{
			name:     "single quoted argument",
			input:    "echo 'hello world'",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "double quoted argument",
			input:    `echo "hello world"`,
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "mixed quotes",
			input:    `echo "hello" 'world'`,
			expected: []string{"echo", "hello", "world"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single word",
			input:    "test",
			expected: []string{"test"},
		},
		{
			name:     "command with equals sign",
			input:    "run --version=1.14.3 --ip=127.0.0.1",
			expected: []string{"run", "--version=1.14.3", "--ip=127.0.0.1"},
		},
		{
			name:     "command with special characters in quotes",
			input:    `echo "hello=world" "--param=value with spaces"`,
			expected: []string{"echo", "hello=world", "--param=value with spaces"},
		},
		{
			name:     "multiple spaces between arguments",
			input:    "echo    hello    world",
			expected: []string{"echo", "hello", "world"},
		},
		{
			name:     "leading and trailing spaces",
			input:    "  echo hello  ",
			expected: []string{"echo", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Split(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSplit_Error(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "unclosed single quote",
			input: "echo 'hello",
		},
		{
			name:  "unclosed double quote",
			input: `echo "hello`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Split(tt.input)
			assert.Error(t, err)
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name     string
		words    []string
		expected string
	}{
		{
			name:     "simple words",
			words:    []string{"echo", "hello"},
			expected: "echo hello",
		},
		{
			name:     "word with spaces",
			words:    []string{"echo", "hello world"},
			expected: `echo 'hello world'`,
		},
		{
			name:     "empty slice",
			words:    []string{},
			expected: "",
		},
		{
			name:     "single word",
			words:    []string{"test"},
			expected: "test",
		},
		{
			name:     "words with special characters",
			words:    []string{"echo", "--param=value"},
			expected: "echo --param=value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Join(tt.words...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSplitJoinRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple command",
			input: "echo hello world",
		},
		{
			name:  "command with flags",
			input: "ls -la /tmp",
		},
		{
			name:  "command with equals",
			input: "run --version=1.14.3 --ip=127.0.0.1",
		},
		{
			name:  "command with shortcodes",
			input: "run --version={version} --ip={host}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			words, err := Split(tt.input)
			require.NoError(t, err)

			joined := Join(words...)
			wordsAgain, err := Split(joined)
			require.NoError(t, err)

			assert.Equal(t, words, wordsAgain)
		})
	}
}
