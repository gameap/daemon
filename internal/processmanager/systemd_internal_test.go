//go:build linux

package processmanager

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_makeCommand(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(errors.WithMessage(err, "failed to create temp dir"))
	}

	tests := []struct {
		name            string
		server          func() *domain.Server
		expectedCommand string
		expectedError   string
	}{
		{
			name: "success with local file",
			server: func() *domain.Server {
				f, err := os.OpenFile(filepath.Join(tempDir, "start.sh"), os.O_CREATE, 0755)
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to create start.sh"))
				}
				err = f.Close()
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to close start.sh"))
				}

				return makeServerWithStartCommandAndDir("start.sh start command", tempDir)
			},
			expectedCommand: filepath.Join(tempDir, "start.sh start command"),
		},
		{
			name: "success with local file and dot",
			server: func() *domain.Server {
				f, err := os.OpenFile(filepath.Join(tempDir, "start.sh"), os.O_CREATE, 0755)
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to create start.sh"))
				}
				err = f.Close()
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to close start.sh"))
				}

				return makeServerWithStartCommandAndDir("./start.sh start command", tempDir)
			},
			expectedCommand: filepath.Join(tempDir, "start.sh start command"),
		},
		{
			name: "success with local file and quotes",
			server: func() *domain.Server {
				f, err := os.OpenFile(filepath.Join(tempDir, "start.sh"), os.O_CREATE, 0755)
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to create start.sh"))
				}
				err = f.Close()
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to close start.sh"))
				}

				return makeServerWithStartCommandAndDir("./start.sh 'some quotes' \"some quotes\" args", tempDir)
			},
			expectedCommand: filepath.Join(tempDir, "./start.sh 'some quotes' 'some quotes' args"),
		},
		{
			name: "success with global file",
			server: func() *domain.Server {
				return makeServerWithStartCommandAndDir("env --help", tempDir)
			},
			expectedCommand: "/usr/bin/env --help",
		},
		{
			name: "success with abs path",
			server: func() *domain.Server {
				return makeServerWithStartCommandAndDir("/usr/bin/env --help", tempDir)
			},
			expectedCommand: "/usr/bin/env --help",
		},
		{
			name: "success with both existing file",
			server: func() *domain.Server {
				f, err := os.OpenFile(filepath.Join(tempDir, "env"), os.O_CREATE, 0755)
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to create env file"))
				}
				err = f.Close()
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to close env file"))
				}

				return makeServerWithStartCommandAndDir("env --help", tempDir)
			},
			expectedCommand: filepath.Join(tempDir, "env --help"),
		},
		{
			name: "success with both existing file and dot",
			server: func() *domain.Server {
				f, err := os.OpenFile(filepath.Join(tempDir, "env"), os.O_CREATE, 0755)
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to create env file"))
				}
				err = f.Close()
				if err != nil {
					t.Fatal(errors.WithMessage(err, "failed to close env file"))
				}

				return makeServerWithStartCommandAndDir("./env --help", tempDir)
			},
			expectedCommand: filepath.Join(tempDir, "env --help"),
		},
		{
			name: "error invalid command",
			server: func() *domain.Server {
				return makeServerWithStartCommandAndDir("invalid", tempDir)
			},
			expectedError: `failed to find command 'invalid'`,
		},
		{
			name: "error invalid global command",
			server: func() *domain.Server {
				return makeServerWithStartCommandAndDir("/usr/bin/invalid", tempDir)
			},
			expectedError: `failed to find command '/usr/bin/invalid'`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := filepath.Walk(tempDir, func(path string, _ os.FileInfo, err error) error {
				if tempDir == path {
					return nil
				}

				if err != nil {
					return err
				}
				return os.RemoveAll(path)
			})
			if err != nil {
				t.Fatal(errors.WithMessage(err, "failed to remove temp dir content"))
			}

			server := test.server()
			systemd := NewSystemD(&config.Config{
				WorkPath: "",
				Scripts: config.Scripts{
					Start: "{command}",
				},
			}, nil, nil)
			command, err := systemd.makeStartCommand(server)

			if test.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expectedCommand, command)
			}
		})
	}
}

func Test_makeCommand_emptyScriptsStart(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)

	server := makeServerWithStartCommandAndDir("env --help", tempDir)
	systemd := NewSystemD(&config.Config{
		WorkPath: "",
		Scripts: config.Scripts{
			Start: "",
		},
	}, nil, nil)

	_, err = systemd.makeStartCommand(server)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyCommand)
}

func Test_systemctl_prefix(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		action   string
		target   string
		expected string
	}{
		{
			name:     "system scope with target",
			scope:    "",
			action:   "start",
			target:   "gameap-server-xid.service",
			expected: "systemctl start gameap-server-xid.service",
		},
		{
			name:     "user scope with target",
			scope:    "user",
			action:   "start",
			target:   "gameap-server-xid.service",
			expected: "systemctl --user start gameap-server-xid.service",
		},
		{
			name:     "system scope without target",
			scope:    "",
			action:   "daemon-reload",
			target:   "",
			expected: "systemctl daemon-reload",
		},
		{
			name:     "user scope without target",
			scope:    "user",
			action:   "daemon-reload",
			target:   "",
			expected: "systemctl --user daemon-reload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeConfigWithScope(tt.scope)
			pm := NewSystemD(cfg, nil, nil)
			assert.Equal(t, tt.expected, pm.systemctl(tt.action, tt.target))
		})
	}
}

func Test_serviceFile_respectsScope(t *testing.T) {
	cur, err := user.Current()
	require.NoError(t, err)

	server := makeServerWithStartCommandAndDir("env --help", t.TempDir())

	t.Run("system scope uses /etc/systemd/system", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope(""), nil, nil)
		assert.True(t,
			strings.HasPrefix(pm.serviceFile(server), "/etc/systemd/system/"),
			"got %s", pm.serviceFile(server),
		)
	})

	t.Run("user scope uses home subpath", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope("user"), nil, nil)
		expectedPrefix := filepath.Join(cur.HomeDir, systemdUserUnitSubpath)
		assert.True(t,
			strings.HasPrefix(pm.serviceFile(server), expectedPrefix+"/"),
			"got %s, expected prefix %s", pm.serviceFile(server), expectedPrefix,
		)
		assert.True(t,
			strings.HasPrefix(pm.socketFile(server), expectedPrefix+"/"),
			"got %s, expected prefix %s", pm.socketFile(server), expectedPrefix,
		)
	})
}

func Test_requireUserMatch(t *testing.T) {
	cur, err := user.Current()
	require.NoError(t, err)

	t.Run("system scope ignores mismatch", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope(""), nil, nil)
		server := makeServerWithStartCommandAndDir("env --help", t.TempDir())
		assert.NoError(t, pm.requireUserMatch(server))
	})

	t.Run("user scope rejects mismatched user", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope("user"), nil, nil)
		server := makeServerWithUser("definitely-not-the-test-runner-user", t.TempDir())
		err := pm.requireUserMatch(server)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUserMismatch)
	})

	t.Run("user scope accepts matching user", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope("user"), nil, nil)
		server := makeServerWithUser(cur.Username, t.TempDir())
		assert.NoError(t, pm.requireUserMatch(server))
	})

	t.Run("user scope accepts empty user", func(t *testing.T) {
		pm := NewSystemD(makeConfigWithScope("user"), nil, nil)
		server := makeServerWithUser("", t.TempDir())
		assert.NoError(t, pm.requireUserMatch(server))
	})
}

func Test_buildServiceConfig_scopeDifferences(t *testing.T) {
	tempDir := t.TempDir()

	f, err := os.OpenFile(filepath.Join(tempDir, "start.sh"), os.O_CREATE, 0755)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	cur, err := user.Current()
	require.NoError(t, err)

	server := domain.NewServer(
		1337, true, domain.ServerInstalled, false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{StartCode: "cstrike"},
		domain.GameMod{Name: "public"},
		"1.3.3.7", 1337, 1338, 1339, "paS$w0rD",
		tempDir,
		cur.Username,
		"./start.sh",
		"", "", "",
		true, time.Now(),
		map[string]string{}, map[string]string{},
		time.Now(),
		0, 0,
	)

	t.Run("system scope emits User/Group and multi-user.target", func(t *testing.T) {
		cfg := makeConfigWithScope("")
		cfg.Scripts.Start = "{command}"
		pm := NewSystemD(cfg, nil, nil)

		got, err := pm.buildServiceConfig(server)
		require.NoError(t, err)
		assert.Contains(t, got, "User=")
		assert.Contains(t, got, "Group=")
		assert.Contains(t, got, "WantedBy=multi-user.target")
		assert.NotContains(t, got, "WantedBy=default.target")
	})

	t.Run("user scope omits User/Group and uses default.target", func(t *testing.T) {
		cfg := makeConfigWithScope("user")
		cfg.Scripts.Start = "{command}"
		pm := NewSystemD(cfg, nil, nil)

		got, err := pm.buildServiceConfig(server)
		require.NoError(t, err)
		assert.NotContains(t, got, "\nUser=")
		assert.NotContains(t, got, "\nGroup=")
		assert.Contains(t, got, "WantedBy=default.target")
		assert.NotContains(t, got, "WantedBy=multi-user.target")
	})
}

func makeConfigWithScope(scope string) *config.Config {
	cfg := &config.Config{
		WorkPath: "",
		Scripts: config.Scripts{
			Start: "{command}",
		},
	}
	if scope != "" {
		cfg.ProcessManager.Config = map[string]string{"scope": scope}
	}
	return cfg
}

func makeServerWithUser(username, dir string) *domain.Server {
	return domain.NewServer(
		1337, true, domain.ServerInstalled, false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{StartCode: "cstrike"},
		domain.GameMod{Name: "public"},
		"1.3.3.7", 1337, 1338, 1339, "paS$w0rD",
		dir,
		username,
		"env --help",
		"", "", "",
		true, time.Now(),
		map[string]string{}, map[string]string{},
		time.Now(),
		0, 0,
	)
}

func makeServerWithStartCommandAndDir(startCommand, dir string) *domain.Server {
	return domain.NewServer(
		1337,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{
			StartCode: "cstrike",
		},
		domain.GameMod{
			Name: "public",
		},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		dir,
		"gameap-user",
		startCommand,
		"",
		"",
		"",
		true,
		time.Now(),
		map[string]string{
			"default_map": "de_dust2",
			"tickrate":    "1000",
		},
		map[string]string{},
		time.Now(),
		0, // cpuLimit
		0, // ramLimit
	)
}
