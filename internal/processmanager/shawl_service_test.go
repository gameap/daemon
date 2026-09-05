package processmanager

import (
	"bufio"
	"strings"
	"testing"

	"github.com/gameap/gameapctl/pkg/oscore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func networkServiceFingerprint() shawlServiceFingerprint {
	return shawlServiceFingerprint{
		ServiceName:        "gameapServer1",
		Account:            oscore.WindowsNetworkServiceAccount,
		NetworkServiceUser: true,
		WorkDir:            `C:\gameap\servers\srv1`,
		BinaryPathName:     `"C:\gameap\shawl.exe" run --name gameapServer1`,
		Command:            `C:\gameap\servers\srv1\srcds.exe -game cstrike`,
	}
}

func TestShawlServiceFingerprint_String(t *testing.T) {
	got := networkServiceFingerprint().String()

	assert.Equal(t, strings.Join([]string{
		"version=2",
		"service=gameapServer1",
		`account=NT AUTHORITY\NetworkService`,
		"network_service_user=true",
		`workdir=C:\gameap\servers\srv1`,
		`binpath="C:\gameap\shawl.exe" run --name gameapServer1`,
		`command=C:\gameap\servers\srv1\srcds.exe -game cstrike`,
		"",
	}, "\n"), got)
}

func TestShawlServiceFingerprint_VersionIsFirstLine(t *testing.T) {
	// The version line tells a reader which daemon wrote the file, and distinguishes it from
	// the older command/workdir/user format.
	got := networkServiceFingerprint().String()

	assert.True(t, strings.HasPrefix(got, "version=2\n"))
}

func TestShawlServiceFingerprint_DoesNotMatchPreviousFormat(t *testing.T) {
	previous := "command=" + `C:\gameap\servers\srv1\srcds.exe -game cstrike` + "\n" +
		"workdir=" + `C:\gameap\servers\srv1` + "\n" +
		"user=gameap\n"

	assert.NotEqual(t, previous, networkServiceFingerprint().String())
}

func TestShawlServiceFingerprint_ChangesWithEveryField(t *testing.T) {
	base := networkServiceFingerprint()

	tests := []struct {
		name   string
		mutate func(f *shawlServiceFingerprint)
	}{
		{"service", func(f *shawlServiceFingerprint) { f.ServiceName = "gameapServer2" }},
		{"account", func(f *shawlServiceFingerprint) { f.Account = "gameap" }},
		{"network_service_user", func(f *shawlServiceFingerprint) { f.NetworkServiceUser = false }},
		{"workdir", func(f *shawlServiceFingerprint) { f.WorkDir = `C:\gameap\servers\srv2` }},
		{"binpath", func(f *shawlServiceFingerprint) { f.BinaryPathName = "other" }},
		{"command", func(f *shawlServiceFingerprint) { f.Command = "other" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := base
			tt.mutate(&changed)

			assert.NotEqual(t, base.String(), changed.String())
		})
	}
}

func TestShawlServiceFingerprint_CarriesNoPassword(t *testing.T) {
	// The marker file lives in C:\gameap\services; a password there would be readable by
	// anything that can read the directory.
	got := networkServiceFingerprint().String()

	for _, line := range strings.Split(got, "\n") {
		assert.False(t, strings.HasPrefix(line, "password"), "unexpected password line %q", line)
	}
	assert.NotContains(t, got, "password")
}

func TestBuildShawlRunArgs(t *testing.T) {
	const (
		serviceName = "gameapServer1"
		workDir     = `C:\gameap\servers\srv1`
		logDir      = `C:\gameap\services\logs`
	)

	prefix := []string{
		"run",
		"--name", serviceName,
		"--restart",
		"--stop-timeout", "10000",
		"--cwd", workDir,
		"--log-dir", logDir,
		"--log-as", serviceName + ".log",
		"--log-rotate", "daily",
		"--log-retain", "7",
		"--",
	}

	tests := []struct {
		name     string
		cmdArr   []string
		expected []string
	}{
		{
			name:     "executable",
			cmdArr:   []string{`C:\gameap\servers\srv1\srcds.exe`, "-game", "cstrike"},
			expected: append(append([]string{}, prefix...), `C:\gameap\servers\srv1\srcds.exe`, "-game", "cstrike"),
		},
		{
			name:     "batch_file_runs_through_cmd",
			cmdArr:   []string{`C:\gameap\servers\srv1\start.bat`, "-console"},
			expected: append(append([]string{}, prefix...), "cmd.exe", "/c", `C:\gameap\servers\srv1\start.bat`, "-console"),
		},
		{
			name:     "cmd_script_runs_through_cmd",
			cmdArr:   []string{`C:\gameap\servers\srv1\start.cmd`, "-console"},
			expected: append(append([]string{}, prefix...), "cmd.exe", "/c", `C:\gameap\servers\srv1\start.cmd`, "-console"),
		},
		{
			name:     "cmd_script_extension_is_case_insensitive",
			cmdArr:   []string{`C:\gameap\servers\srv1\start.CMD`},
			expected: append(append([]string{}, prefix...), "cmd.exe", "/c", `C:\gameap\servers\srv1\start.CMD`),
		},
		{
			name:     "batch_file_extension_is_case_insensitive",
			cmdArr:   []string{`C:\gameap\servers\srv1\start.BAT`},
			expected: append(append([]string{}, prefix...), "cmd.exe", "/c", `C:\gameap\servers\srv1\start.BAT`),
		},
		{
			name:     "argument_with_spaces_stays_one_element",
			cmdArr:   []string{`C:\srv\srcds.exe`, "+hostname", "My Server"},
			expected: append(append([]string{}, prefix...), `C:\srv\srcds.exe`, "+hostname", "My Server"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildShawlRunArgs(serviceName, workDir, logDir, true, tt.cmdArr)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// The restart flag follows the server's autostart setting: shawl supervises the
// game process itself, so leaving it on would restart a server the operator
// asked not to be restarted.
func TestBuildShawlRunArgs_RestartFollowsAutostart(t *testing.T) {
	cmdArr := []string{`C:\gameap\servers\srv1\srcds.exe`}

	withRestart, err := buildShawlRunArgs("gameapServer42", `C:\srv`, `C:\logs`, true, cmdArr)
	require.NoError(t, err)
	assert.Contains(t, withRestart, "--restart")
	assert.NotContains(t, withRestart, "--no-restart")

	withoutRestart, err := buildShawlRunArgs("gameapServer7", `C:\srv`, `C:\logs`, false, cmdArr)
	require.NoError(t, err)
	assert.Contains(t, withoutRestart, "--no-restart")
	assert.NotContains(t, withoutRestart, "--restart")
}

func TestBuildShawlRunArgs_EmptyCommand(t *testing.T) {
	_, err := buildShawlRunArgs("gameapServer1", `C:\srv`, `C:\logs`, true, nil)

	assert.ErrorIs(t, err, ErrEmptyCommand)
}

func TestServiceStateName(t *testing.T) {
	tests := map[uint32]string{
		1: "STOPPED",
		2: "START_PENDING",
		3: "STOP_PENDING",
		4: "RUNNING",
		5: "CONTINUE_PENDING",
		6: "PAUSE_PENDING",
		7: "PAUSED",
		9: "UNKNOWN(9)",
	}

	for state, expected := range tests {
		assert.Equal(t, expected, serviceStateName(state))
	}
}

func TestServiceErrorSymbol(t *testing.T) {
	assert.Equal(t, "ERROR_SERVICE_DEPENDENCY_FAIL", serviceErrorSymbol(1068))
	assert.Equal(t, "ERROR_SERVICE_LOGON_FAILED", serviceErrorSymbol(1069))
	assert.Equal(t, "ERROR_INVALID_SERVICE_ACCOUNT", serviceErrorSymbol(1057))
	assert.Equal(t, "ERROR_SERVICE_DOES_NOT_EXIST", serviceErrorSymbol(1060))
	assert.Empty(t, serviceErrorSymbol(7))
}

func TestServiceErrorHint(t *testing.T) {
	const logDir = `C:\gameap\services\logs`

	// The hints for a rejected account must name the canonical spelling, since a localized one
	// resolves to the right SID but is not accepted by the service control manager.
	for _, code := range []uint32{1057, 1069} {
		hint := serviceErrorHint(code, `NT AUTHORITY\NETTVERKSTJENESTE`, logDir)

		assert.Contains(t, hint, `NT AUTHORITY\NetworkService`)
		assert.Contains(t, hint, `NT AUTHORITY\NETTVERKSTJENESTE`)
	}

	assert.Contains(t, serviceErrorHint(1068, "gameap", logDir), logDir)
	assert.Empty(t, serviceErrorHint(7, "gameap", logDir))
}

func TestParseShawlLogLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "quoted_payload",
			input:    `2025-11-29 00:07:35 [DEBUG] stdout: "Server started"`,
			expected: "Server started",
		},
		{
			name:     "unquoted_payload",
			input:    `2025-11-29 00:07:35 [INFO] stderr: warning`,
			expected: "warning",
		},
		{
			name:     "embedded_separator_is_kept",
			input:    `2025-11-29 00:07:35 [DEBUG] stdout: "map: de_dust2"`,
			expected: "map: de_dust2",
		},
		{
			name:     "no_bracket_returns_line",
			input:    "plain line",
			expected: "plain line",
		},
		{
			name:     "no_colon_returns_rest",
			input:    "2025-11-29 00:07:35 [DEBUG] no separator here",
			expected: "no separator here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseShawlLogLine(tt.input))
		})
	}
}

func shawlLogLines(messages ...string) string {
	var b strings.Builder

	for _, msg := range messages {
		b.WriteString(`2025-11-29 00:07:35 [DEBUG] stdout: "` + msg + `"` + "\n")
	}

	return b.String()
}

func TestReadShawlLogTail(t *testing.T) {
	tests := []struct {
		name     string
		messages []string
		limit    int
		expected []string
	}{
		{
			name:     "empty_log",
			messages: nil,
			limit:    3,
			expected: []string{},
		},
		{
			name:     "fewer_lines_than_limit",
			messages: []string{"a", "b"},
			limit:    3,
			expected: []string{"a", "b"},
		},
		{
			name:     "exactly_the_limit",
			messages: []string{"a", "b", "c"},
			limit:    3,
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "one_line_over_the_limit",
			messages: []string{"a", "b", "c", "d"},
			limit:    3,
			expected: []string{"b", "c", "d"},
		},
		{
			name:     "wraps_the_window_more_than_once",
			messages: []string{"a", "b", "c", "d", "e", "f", "g"},
			limit:    3,
			expected: []string{"e", "f", "g"},
		},
		{
			name:     "exact_multiple_of_the_limit",
			messages: []string{"a", "b", "c", "d", "e", "f"},
			limit:    3,
			expected: []string{"d", "e", "f"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readShawlLogTail(strings.NewReader(shawlLogLines(tt.messages...)), tt.limit, false)

			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestReadShawlLogTail_SkipsEmptyMessages(t *testing.T) {
	log := shawlLogLines("a") + "\n" + shawlLogLines("b")

	got, err := readShawlLogTail(strings.NewReader(log), 5, false)

	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, got)
}

func TestReadShawlLogTail_NonPositiveLimit(t *testing.T) {
	got, err := readShawlLogTail(strings.NewReader(shawlLogLines("a")), 0, false)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestReadShawlLogTail_ReadsLineLongerThanScannerDefault(t *testing.T) {
	// bufio.Scanner stops at 64 KiB per line by default, which would abort the scan and lose
	// everything after the long line.
	long := strings.Repeat("x", 200*1024)

	got, err := readShawlLogTail(strings.NewReader(shawlLogLines(long, "after")), 2, false)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, long, got[0])
	assert.Equal(t, "after", got[1])
}

func TestReadShawlLogTail_ReportsLineOverTheHardLimit(t *testing.T) {
	got, err := readShawlLogTail(strings.NewReader(strings.Repeat("x", shawlLogMaxLineSize+1)), 2, false)

	assert.ErrorIs(t, err, bufio.ErrTooLong)
	assert.Nil(t, got)
}

func TestReadShawlLogTail_DropsLineSplitByTheReadBoundary(t *testing.T) {
	// A read bounded to the last N bytes starts inside an entry. Its tail is not a log line and
	// parsing it yields a fragment of one, so it must not reach the output.
	full := shawlLogLines("first message", "second message", "third message")
	boundary := strings.Index(full, "first message") + len("first ")

	got, err := readShawlLogTail(strings.NewReader(full[boundary:]), 5, true)

	require.NoError(t, err)
	assert.Equal(t, []string{"second message", "third message"}, got)
}

func TestReadShawlLogTail_KeepsFirstLineWhenNotSeeking(t *testing.T) {
	full := shawlLogLines("first message", "second message")

	got, err := readShawlLogTail(strings.NewReader(full), 5, false)

	require.NoError(t, err)
	assert.Equal(t, []string{"first message", "second message"}, got)
}

func TestReadShawlLogTail_BoundaryInsideTheOnlyLine(t *testing.T) {
	full := shawlLogLines("only message")
	boundary := strings.Index(full, "only message") + len("only ")

	got, err := readShawlLogTail(strings.NewReader(full[boundary:]), 5, true)

	require.NoError(t, err)
	assert.Empty(t, got)
}
