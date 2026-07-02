package shellquote

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWindowsArgToString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", `""`},
		{"no_special_chars", "simple", "simple"},
		{"backslashes_no_space", `C:\path\to`, `C:\path\to`},
		{"space", "a b", `"a b"`},
		{"single_quote_and_space", "Andrey's Server", `"Andrey's Server"`},
		{"embedded_quote", `a"b`, `"a\"b"`},
		{"quote_only", `"`, `"\""`},
		{"space_and_trailing_backslash", `C:\path with space\`, `"C:\path with space\\"`},
		{"space_and_backslash_before_quote", `a \"b`, `"a \\\"b"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, WindowsArgToString(tt.input))
		})
	}
}

func TestWindowsJoin(t *testing.T) {
	got := WindowsJoin("game.exe", "--name", "Andrey's Server", "--path", `C:\a b\`)

	assert.Equal(t, `game.exe --name "Andrey's Server" --path "C:\a b\\"`, got)
}
