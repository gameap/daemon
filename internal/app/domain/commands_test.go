package domain

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeWorkDirReader struct {
	workDir string
}

func (f fakeWorkDirReader) WorkDir() string {
	return f.workDir
}

func TestReplaceShortCodes_ReplacesBuiltinPlaceholders(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, nil, nil)

	template := "ip={ip} port={port} query_port={query_port} rcon_port={rcon_port} " +
		"rcon_password={rcon_password} dir={dir} uuid={uuid} id={id}"

	result, err := ReplaceShortCodes(template, cfg, server)

	require.NoError(t, err)
	assert.Equal(t,
		"ip=127.0.0.1 port=27015 query_port=27016 rcon_port=27017 "+
			"rcon_password=rconpass dir=/work-path/server-dir uuid=test-uuid id=1",
		result,
	)
}

func TestReplaceShortCodes_ResolvesHostnameFromGameModDefault(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
		},
		nil,
		nil,
	)

	result, err := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

	require.NoError(t, err)
	assert.Equal(t, "+set hostname 'Default Hostname'", result)
}

func TestReplaceShortCodes_ResolvesHostnameFromServerVars(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
		},
		map[string]string{"hostname": "Vars Hostname"},
		nil,
	)

	result, err := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

	require.NoError(t, err)
	assert.Equal(t, "+set hostname 'Vars Hostname'", result)
}

func TestReplaceShortCodes_ResolvesHostnameFromServerSettings(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
		},
		map[string]string{"hostname": "Vars Hostname"},
		Settings{"hostname": "Settings Hostname"},
	)

	result, err := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

	require.NoError(t, err)
	assert.Equal(t, "+set hostname 'Settings Hostname'", result)
}

func TestReplaceShortCodes_CaseInsensitivePlaceholder(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
		},
		nil,
		Settings{"hostname": "Settings Hostname"},
	)

	result, err := ReplaceShortCodes("+set hostname '{HOSTNAME}'", cfg, server)

	require.NoError(t, err)
	assert.Equal(t, "+set hostname 'Settings Hostname'", result)
}

func TestMakeFullCommand_InjectsServerCommand(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(
		[]GameModVarTemplate{
			{Key: "hostname", DefaultValue: "Default Hostname"},
		},
		nil,
		Settings{"hostname": "Settings Hostname"},
	)

	result, err := MakeFullCommand(
		cfg,
		server,
		"./wrapper --ip {ip} -- {command}",
		"./run.sh +set hostname '{hostname}'",
	)

	require.NoError(t, err)
	assert.Equal(t,
		"./wrapper --ip 127.0.0.1 -- ./run.sh +set hostname 'Settings Hostname'",
		result,
	)
}

func TestReplaceShortCodes_WorkDirDefaultsToServerDir(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, nil, nil)

	result, err := ReplaceShortCodes("dir={dir} work_dir={work_dir}", cfg, server)

	require.NoError(t, err)
	assert.Equal(t,
		"dir="+filepath.Join("/work-path", "server-dir")+" work_dir="+filepath.Join("/work-path", "server-dir"),
		result,
	)
}

func TestReplaceShortCodes_WorkDirPlaceholderIsAbsoluteEvenWhenSetAsServerVar(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"work_dir": "bin/x"}, nil)

	result, err := ReplaceShortCodes("dir={dir} work_dir={work_dir}", cfg, server)

	require.NoError(t, err)
	assert.Equal(t,
		"dir="+filepath.Join("/work-path", "server-dir")+" work_dir="+filepath.Join("/work-path", "server-dir", "bin", "x"),
		result,
	)
}

func TestReplaceShortCodes_InvalidWorkDirReturnsError(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"work_dir": "../other"}, nil)

	_, err := ReplaceShortCodes("{work_dir}", cfg, server)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to resolve server process work directory")
	assert.Contains(t, err.Error(), `invalid work_dir "../other" from server vars`)
	assert.Contains(t, err.Error(), "path is outside work directory")
}

func TestMakeFullCommand_InvalidWorkDirReturnsError(t *testing.T) {
	cfg := fakeWorkDirReader{workDir: "/work-path"}
	server := newTestServerForVars(nil, map[string]string{"work_dir": "/abs"}, nil)

	_, err := MakeFullCommand(cfg, server, "{command}", "./run.sh")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "work_dir must be a path relative to the server directory")
}
