//go:build linux

package processmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemdQuoteArg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain", "hlds_run", "hlds_run"},
		{"empty", "", `""`},
		{"space", "Andrey Server", `"Andrey Server"`},
		{"single_quote_and_space", "Andrey's Server", `"Andrey's Server"`},
		{"double_quote", `a"b`, `"a\"b"`},
		{"backslash", `a\b`, `"a\\b"`},
		{"dollar", "$HOME", "$$HOME"},
		{"percent", "100%", "100%%"},
		{"dollar_with_space", "$x y", `"$$x y"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, systemdQuoteArg(tt.input))
		})
	}
}

func TestSystemdQuoteArgs(t *testing.T) {
	got := systemdQuoteArgs([]string{"/usr/bin/srv", "--name", "Andrey's Server", "--pct", "100%"})

	assert.Equal(t, `/usr/bin/srv --name "Andrey's Server" --pct 100%%`, got)
}
