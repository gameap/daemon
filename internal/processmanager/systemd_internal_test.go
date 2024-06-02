//go:build linux
// +build linux

package processmanager

import (
	"os"
	"path/filepath"
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
	)
}
