package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCommandArgs_KeepsSubstitutedValueAsSingleArgument(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}

	tests := []struct {
		name     string
		hostname string
	}{
		{"single_quote_and_space", "Andrey's Server"},
		{"dollars_quotes_and_space", `$$$ Andrey's Server"$$"`},
		{"command_substitution", "$(id)"},
		{"backticks", "`reboot`"},
		{"shell_metacharacters", "a; rm -rf / | cat"},
		{"leading_and_trailing_spaces", "  padded  "},
		{"percent_and_variable", "100% $HOME"},
		{"backslashes", `C:\path\to\thing`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServerForVars(nil, map[string]string{"hostname": tt.hostname}, nil)

			args, err := BuildCommandArgs(cfg, server, "{command}", "./run --name {hostname}")

			require.NoError(t, err)
			require.Len(t, args, 3)
			assert.Equal(t, "./run", args[0])
			assert.Equal(t, "--name", args[1])
			assert.Equal(t, tt.hostname, args[2])
		})
	}
}

func TestBuildCommandArgs_QuotedPlaceholderStaysSingleArgument(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"hostname": "My Server"}, nil)

	args, err := BuildCommandArgs(cfg, server, "{command}", "./run --name '{hostname}'")

	require.NoError(t, err)
	require.Len(t, args, 3)
	assert.Equal(t, "My Server", args[2])
}

func TestBuildCommandArgs_SplicesCommandTokensIntoWrapper(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"hostname": "My Server"}, nil)

	args, err := BuildCommandArgs(
		cfg, server,
		"./wrapper --ip {ip} -- {command}",
		"./run.sh +set hostname '{hostname}'",
	)

	require.NoError(t, err)
	require.Equal(t,
		[]string{"./wrapper", "--ip", "127.0.0.1", "--", "./run.sh", "+set", "hostname", "My Server"},
		args,
	)
}

func TestBuildCommandArgs_DoesNotReexpandSubstitutedValue(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"hostname": "{id}"}, nil)

	args, err := BuildCommandArgs(cfg, server, "{command}", "./run --name {hostname}")

	require.NoError(t, err)
	require.Len(t, args, 3)
	assert.Equal(t, "{id}", args[2])
}

func TestBuildCommandArgs_EmptyServerCommandYieldsNoArguments(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, nil, nil)

	args, err := BuildCommandArgs(cfg, server, "{command}", "")

	require.NoError(t, err)
	require.Empty(t, args)
}

func TestBuildCommandArgs_ReportsUnbalancedQuoteInTemplate(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, nil, nil)

	_, err := BuildCommandArgs(cfg, server, "{command}", "./run --name 'unterminated")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to split server command")
}
