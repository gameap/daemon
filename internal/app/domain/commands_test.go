package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	result := ReplaceShortCodes(template, cfg, server)

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

	result := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

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

	result := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

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

	result := ReplaceShortCodes("+set hostname '{hostname}'", cfg, server)

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

	result := ReplaceShortCodes("+set hostname '{HOSTNAME}'", cfg, server)

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

	result := MakeFullCommand(
		cfg,
		server,
		"./wrapper --ip {ip} -- {command}",
		"./run.sh +set hostname '{hostname}'",
	)

	assert.Equal(t,
		"./wrapper --ip 127.0.0.1 -- ./run.sh +set hostname 'Settings Hostname'",
		result,
	)
}
