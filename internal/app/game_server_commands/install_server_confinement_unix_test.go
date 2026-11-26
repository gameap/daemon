//go:build linux || darwin

package gameservercommands

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests of this file are about one thing: an installation runs with the
// privileges of the daemon inside a directory the tenant of the game server
// writes to, and nothing the tenant leaves there may carry what the
// installation does out of that directory.

const outsideContent = "root:x:0:0"

// installStand is a work directory with the directory of one game server in
// it, and next to the work directory a directory of the node that no
// installation may touch.
type installStand struct {
	cfg       *config.Config
	server    *domain.Server
	serverDir string
	// outside is a directory out of the work directory and secret the one
	// file in it.
	outside  string
	secret   string
	output   *bytes.Buffer
	executor *testExecutor
}

func givenInstallStand(t *testing.T) *installStand {
	t.Helper()

	base := t.TempDir()
	workPath := filepath.Join(base, "work")
	outside := filepath.Join(base, "outside")
	secret := filepath.Join(outside, "passwd")

	require.NoError(t, os.MkdirAll(filepath.Join(workPath, "test-server"), 0o755))
	require.NoError(t, os.Mkdir(outside, 0o755))
	require.NoError(t, os.WriteFile(secret, []byte(outsideContent), 0o600))

	return &installStand{
		cfg:       &config.Config{WorkPath: workPath},
		server:    givenServer(t, domain.Game{StartCode: "test"}, domain.GameMod{Name: "test"}),
		serverDir: filepath.Join(workPath, "test-server"),
		outside:   outside,
		secret:    secret,
		output:    &bytes.Buffer{},
		executor:  &testExecutor{},
	}
}

// installatorKinds are the two ways the installation is run: the first
// installation of a game server and its update.
var installatorKinds = map[string]func(*config.Config, contracts.Executor, io.ReadWriter) *installator{
	"installer": func(cfg *config.Config, executor contracts.Executor, output io.ReadWriter) *installator {
		return newInstallator(cfg, executor, output)
	},
	"updater": func(cfg *config.Config, executor contracts.Executor, output io.ReadWriter) *installator {
		return newUpdater(cfg, executor, output)
	},
}

func (s *installStand) install(kind string, rules ...*installationRule) error {
	return installatorKinds[kind](s.cfg, s.executor, s.output).Install(context.Background(), s.server, rules)
}

// assertOutsideUntouched checks that the directory out of the work directory
// holds what it held: the one file, with its content and mode.
func (s *installStand) assertOutsideUntouched(t *testing.T) {
	t.Helper()

	entries, err := os.ReadDir(s.outside)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	require.Equal(t, []string{"passwd"}, names, "nothing may be created out of the work directory")

	info, err := os.Lstat(s.secret)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode(), "the file out of the work directory keeps its type and mode")

	content, err := os.ReadFile(s.secret)
	require.NoError(t, err)
	assert.Equal(t, outsideContent, string(content), "the file out of the work directory keeps its content")
}

func assertInstalledFile(t *testing.T, path, wantContent string, wantPerm os.FileMode) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "%s has to be a regular file, it is %s", path, info.Mode())
	assert.Equal(t, wantPerm, info.Mode().Perm(), path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, wantContent, string(content), path)
}

func assertInstalledSymlink(t *testing.T, path, wantTarget string) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "%s has to be a symlink, it is %s", path, info.Mode())

	target, err := os.Readlink(path)
	require.NoError(t, err)
	assert.Equal(t, wantTarget, target)
}

func localArchiveRule(path string) *installationRule {
	return &installationRule{SourceValue: path, Action: unpackFromLocalRepository}
}

func localDirectoryRule(path string) *installationRule {
	return &installationRule{SourceValue: path, Action: copyDirectoryFromLocalRepository}
}

func remoteRule(source string) *installationRule {
	return &installationRule{SourceValue: source, Action: downloadAnUnpackFromRemoteRepository}
}

// steamRule installs the Half-Life Dedicated Server, application 90 of Steam.
func steamRule() *installationRule {
	return &installationRule{SourceValue: "90", Action: installFromSteam}
}

func localRepositoryFixture(t *testing.T, name string) string {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "test", "files", "local_repository", name))
	require.NoError(t, err)

	return path
}

// givenRemoteRepository serves the files of dir the way a remote repository
// does and returns its address.
func givenRemoteRepository(t *testing.T, dir string) string {
	t.Helper()

	server := httptest.NewServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(server.Close)

	return server.URL
}

// deliveries are the two ways a repository file reaches the installation: read
// from the disk of the node, or downloaded. Each returns the rule that installs
// the file path names.
var deliveries = map[string]func(t *testing.T, path string) *installationRule{
	"local": func(_ *testing.T, path string) *installationRule {
		return localArchiveRule(path)
	},
	"remote": func(t *testing.T, path string) *installationRule {
		t.Helper()

		return remoteRule(givenRemoteRepository(t, filepath.Dir(path)) + "/" + filepath.Base(path))
	},
}

// archiveEntry is one entry for givenTarGz. An entry with a linkname is a
// symlink, one whose name ends in a slash a directory, any other a file.
type archiveEntry struct {
	name     string
	body     string
	mode     int64
	linkname string
}

// givenTarGz writes a tar.gz with the entries in the order given and returns
// its path.
func givenTarGz(t *testing.T, name string, entries ...archiveEntry) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)

	file, err := os.Create(path)
	require.NoError(t, err)

	gzWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzWriter)

	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body))}

		switch {
		case entry.linkname != "":
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.linkname
			header.Mode = 0o777
		case strings.HasSuffix(entry.name, "/"):
			header.Typeflag = tar.TypeDir
		default:
			header.Typeflag = tar.TypeReg
		}

		if header.Mode == 0 {
			header.Mode = 0o644
			if header.Typeflag == tar.TypeDir {
				header.Mode = 0o755
			}
		}

		require.NoError(t, tarWriter.WriteHeader(header))

		_, err := tarWriter.Write([]byte(entry.body))
		require.NoError(t, err)
	}

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzWriter.Close())
	require.NoError(t, file.Close())

	return path
}

// The report this file answers to: a game server leaves a symlink under the
// name of a file its game archive holds, and the next installation or update,
// unpacking as root, writes the file to where the link leads.
func TestInstallator_ArchiveFileMeetsASymlink(t *testing.T) {
	t.Parallel()

	archives := []struct {
		name  string
		file  string
		entry string
	}{
		{name: "tar_gz", file: "game.tar.gz", entry: "game_file_from_tar_gz"},
		{name: "tar_xz", file: "game.tar.xz", entry: "game_file_from_tar_xz"},
		{name: "zip", file: "game.zip", entry: "game_file_from_zip"},
	}

	links := map[string]func(s *installStand) string{
		"link_to_a_file_outside": func(s *installStand) string {
			return s.secret
		},
		"relative_link_to_a_file_outside": func(*installStand) string {
			return "../../outside/passwd"
		},
		"link_to_a_missing_file_outside": func(s *installStand) string {
			return filepath.Join(s.outside, "ld.so.preload")
		},
	}

	for _, archive := range archives {
		for linkName, link := range links {
			for deliveryName, delivery := range deliveries {
				for kind := range installatorKinds {
					name := strings.Join([]string{archive.name, linkName, deliveryName, kind}, "/")

					t.Run(name, func(t *testing.T) {
						t.Parallel()

						stand := givenInstallStand(t)
						require.NoError(t, os.Symlink(link(stand), filepath.Join(stand.serverDir, archive.entry)))

						err := stand.install(kind, delivery(t, localRepositoryFixture(t, archive.file)))

						stand.assertOutsideUntouched(t)
						require.NoError(t, err)
						assertInstalledFile(t, filepath.Join(stand.serverDir, archive.entry), archive.entry+"\n", 0o644)
					})
				}
			}
		}
	}
}

// A symlink of a tar archive used to be unpacked as an empty file with the
// mode of the link, 0777, through whatever stood under its name: enough to
// create a file anywhere on the node and leave it writable for everybody. The
// archives of Counter-Strike and Quake 2 carry such entries.
func TestInstallator_ArchiveSymlinkMeetsASymlink(t *testing.T) {
	t.Parallel()

	archive := givenTarGz(t, "hlcs_base.tar.gz",
		archiveEntry{name: "./"},
		archiveEntry{name: "./libSDL2.so", linkname: "libSDL2-2.0.so.0"},
		archiveEntry{name: "./libSDL2-2.0.so.0", body: "ELF", mode: 0o755},
	)

	tests := []struct {
		name string
		// plant leaves something under the name of the symlink entry.
		plant func(t *testing.T, s *installStand)
	}{
		{
			name:  "nothing_stands_under_its_name",
			plant: func(*testing.T, *installStand) {},
		},
		{
			name: "link_to_a_missing_file_outside_stands_under_its_name",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				target := filepath.Join(s.outside, "ld.so.preload")
				require.NoError(t, os.Symlink(target, filepath.Join(s.serverDir, "libSDL2.so")))
			},
		},
		{
			name: "link_to_a_file_outside_stands_under_its_name",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, filepath.Join(s.serverDir, "libSDL2.so")))
			},
		},
	}

	for _, tt := range tests {
		for deliveryName, delivery := range deliveries {
			for kind := range installatorKinds {
				t.Run(strings.Join([]string{tt.name, deliveryName, kind}, "/"), func(t *testing.T) {
					t.Parallel()

					stand := givenInstallStand(t)
					tt.plant(t, stand)

					err := stand.install(kind, delivery(t, archive))

					stand.assertOutsideUntouched(t)
					require.NoError(t, err)
					assertInstalledSymlink(t, filepath.Join(stand.serverDir, "libSDL2.so"), "libSDL2-2.0.so.0")
					assertInstalledFile(t, filepath.Join(stand.serverDir, "libSDL2-2.0.so.0"), "ELF", 0o755)
				})
			}
		}
	}
}

// A symlink in place of a directory of the archive takes every file below it
// along. One that leads out of the server directory is not followed and not
// taken away either: the installation stops and says which link it is.
func TestInstallator_ArchiveDirectoryMeetsASymlink(t *testing.T) {
	t.Parallel()

	archives := map[string]string{
		"archive_names_the_directory": givenTarGz(t, "game.tar.gz",
			archiveEntry{name: "cstrike/"},
			archiveEntry{name: "cstrike/addons/"},
			archiveEntry{name: "cstrike/addons/plugins.ini", body: "plugins"},
		),
		"archive_names_only_the_file_inside": givenTarGz(t, "game.tar.gz",
			archiveEntry{name: "cstrike/addons/plugins.ini", body: "plugins"},
		),
	}

	tests := []struct {
		name      string
		plant     func(t *testing.T, s *installStand)
		wantError string
		wantFile  string
	}{
		{
			name: "link_out_of_the_server_directory",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(s.serverDir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink(s.outside, filepath.Join(s.serverDir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "relative_link_out_of_the_server_directory",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(s.serverDir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink("../../../outside", filepath.Join(s.serverDir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_out_of_the_server_directory_on_the_way",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.outside, filepath.Join(s.serverDir, "cstrike")))
			},
			wantError: `"cstrike" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_to_the_directory_of_another_server",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				neighbour := filepath.Join(s.cfg.WorkPath, "neighbour")
				require.NoError(t, os.Mkdir(neighbour, 0o755))
				require.NoError(t, os.Mkdir(filepath.Join(s.serverDir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink("../../neighbour", filepath.Join(s.serverDir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_to_a_directory_of_the_server_itself",
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.MkdirAll(filepath.Join(s.serverDir, "shared", "addons"), 0o755))
				require.NoError(t, os.Mkdir(filepath.Join(s.serverDir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink("../shared/addons", filepath.Join(s.serverDir, "cstrike", "addons")))
			},
			wantFile: "shared/addons/plugins.ini",
		},
	}

	for _, tt := range tests {
		for archiveName, archive := range archives {
			for deliveryName, delivery := range deliveries {
				t.Run(strings.Join([]string{tt.name, archiveName, deliveryName}, "/"), func(t *testing.T) {
					t.Parallel()

					stand := givenInstallStand(t)
					tt.plant(t, stand)

					err := stand.install("updater", delivery(t, archive))

					stand.assertOutsideUntouched(t)
					assert.NoFileExists(t, filepath.Join(stand.cfg.WorkPath, "neighbour", "plugins.ini"))

					if tt.wantError != "" {
						require.Error(t, err)
						assert.Contains(t, err.Error(), tt.wantError)
						assert.Contains(t, stand.output.String(), tt.wantError, "the tenant is told which link is in the way")

						return
					}

					require.NoError(t, err)
					assertInstalledFile(t, filepath.Join(stand.serverDir, filepath.FromSlash(tt.wantFile)), "plugins", 0o644)
				})
			}
		}
	}
}

// Where the work directory belongs to the user the game servers run as, the
// name of the server directory is the tenant's to replace. A symlink in its
// place used to hand the tree it names to the installation: unpacked into, and
// given to the game server user file by file.
func TestInstallator_ServerDirReplacedByASymlink(t *testing.T) {
	t.Parallel()

	rules := map[string]func(t *testing.T) *installationRule{
		"local_archive": func(t *testing.T) *installationRule {
			t.Helper()

			return localArchiveRule(localRepositoryFixture(t, "game.tar.gz"))
		},
		"remote_archive": func(t *testing.T) *installationRule {
			t.Helper()

			return deliveries["remote"](t, localRepositoryFixture(t, "game.zip"))
		},
		"remote_file": func(t *testing.T) *installationRule {
			t.Helper()

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "server.jar"), []byte("jar"), 0o644))

			return deliveries["remote"](t, filepath.Join(dir, "server.jar"))
		},
		"local_directory": func(t *testing.T) *installationRule {
			t.Helper()

			path, err := filepath.Abs(filepath.Join("..", "..", "..", "test", "files", "directory"))
			require.NoError(t, err)

			return localDirectoryRule(path)
		},
		"steam": func(*testing.T) *installationRule {
			return steamRule()
		},
	}

	links := map[string]func(s *installStand) string{
		"absolute_link": func(s *installStand) string { return s.outside },
		"relative_link": func(*installStand) string { return "../outside" },
	}

	for ruleName, rule := range rules {
		for linkName, link := range links {
			for kind := range installatorKinds {
				t.Run(strings.Join([]string{ruleName, linkName, kind}, "/"), func(t *testing.T) {
					t.Parallel()

					stand := givenInstallStand(t)
					stand.cfg.SteamCMDPath = filepath.Join(stand.cfg.WorkPath, "steamcmd")
					require.NoError(t, os.Mkdir(stand.cfg.SteamCMDPath, 0o755))

					require.NoError(t, os.Remove(stand.serverDir))
					require.NoError(t, os.Symlink(link(stand), stand.serverDir))

					err := stand.install(kind, rule(t))

					stand.assertOutsideUntouched(t)
					require.Error(t, err)
					assert.Contains(t, err.Error(), `failed to open server directory "test-server" inside the work directory`)
					assert.Contains(t, err.Error(), "symlinks that lead out of it are not followed")
					assert.Contains(t, err.Error(), "path escapes from parent")
					assert.Contains(t, stand.output.String(), "symlinks that lead out of it are not followed")
					assert.Empty(t, stand.executor.command, "steamcmd is not started for a directory that was refused")
					assertInstalledSymlink(t, stand.serverDir, link(stand))
				})
			}
		}
	}
}

// The other side of the same rule, written down so that it is not mistaken for
// a protection: the work directory is the boundary, not the server directory.
// A link that stays inside the work directory is followed, as the file manager
// follows it, because whoever can replace the name of a server directory owns
// the work directory already.
func TestInstallator_ServerDirLinkedInsideTheWorkDir(t *testing.T) {
	t.Parallel()

	stand := givenInstallStand(t)
	require.NoError(t, os.Mkdir(filepath.Join(stand.cfg.WorkPath, "moved"), 0o755))
	require.NoError(t, os.Remove(stand.serverDir))
	require.NoError(t, os.Symlink("moved", stand.serverDir))

	err := stand.install("installer", localArchiveRule(localRepositoryFixture(t, "game.tar.gz")))

	require.NoError(t, err)
	stand.assertOutsideUntouched(t)
	assertInstalledFile(t, filepath.Join(stand.cfg.WorkPath, "moved", "game_file_from_tar_gz"), "game_file_from_tar_gz\n", 0o644)
}

func TestInstallator_ServerDirNamedOutOfTheWorkDir(t *testing.T) {
	t.Parallel()

	stand := givenInstallStand(t)
	stand.server = domain.NewServer(
		1, true, domain.ServerInstalled, false, "name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef", "759b875e",
		domain.Game{StartCode: "test"}, domain.GameMod{Name: "test"},
		"1.3.3.7", 1337, 1338, 1339, "paS$w0rD",
		"../outside", "gameap-user",
		"./run.sh", "", "", "", false, time.Now(),
		map[string]string{}, map[string]string{}, time.Now(), 0, 0,
	)

	err := stand.install("installer", localArchiveRule(localRepositoryFixture(t, "game.tar.gz")))

	stand.assertOutsideUntouched(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to open server directory "../outside" inside the work directory`)
	assert.Contains(t, err.Error(), "path is outside work directory")
}

// A file of a repository kept as a directory replaces a symlink standing under
// its name the way a file of an archive does.
func TestInstallator_LocalDirectoryFileMeetsASymlink(t *testing.T) {
	t.Parallel()

	repository, err := filepath.Abs(filepath.Join("..", "..", "..", "test", "files", "directory"))
	require.NoError(t, err)

	links := map[string]func(s *installStand) string{
		"link_to_a_file_outside":         func(s *installStand) string { return s.secret },
		"link_to_a_missing_file_outside": func(s *installStand) string { return filepath.Join(s.outside, "created") },
		"link_to_a_file_of_the_server":   func(*installStand) string { return "banned.cfg" },
	}

	for linkName, link := range links {
		for kind := range installatorKinds {
			t.Run(linkName+"/"+kind, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)
				banned := filepath.Join(stand.serverDir, "banned.cfg")
				require.NoError(t, os.WriteFile(banned, []byte("banid 1"), 0o600))
				require.NoError(t, os.Symlink(link(stand), filepath.Join(stand.serverDir, "directory_file.txt")))

				err := stand.install(kind, localDirectoryRule(repository))

				stand.assertOutsideUntouched(t)
				require.NoError(t, err)
				assertInstalledFile(t, banned, "banid 1", 0o600)
				assertInstalledFile(t, filepath.Join(stand.serverDir, "directory_file.txt"), "directory_file.txt\n", 0o644)
			})
		}
	}
}

func TestInstallator_LocalDirectoryMeetsASymlinkInPlaceOfADirectory(t *testing.T) {
	t.Parallel()

	repository := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(repository, "addons"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repository, "addons", "plugins.ini"), []byte("plugins"), 0o644))

	stand := givenInstallStand(t)
	require.NoError(t, os.Symlink(stand.outside, filepath.Join(stand.serverDir, "addons")))

	err := stand.install("installer", localDirectoryRule(repository))

	stand.assertOutsideUntouched(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy files")
	assert.Contains(t, err.Error(), "path escapes from parent")
}

// A repository file that is no archive is stored as it is, under its own name:
// a jar, a single binary. It replaces what stands under that name like a file
// of an archive does.
func TestInstallator_PlainFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		delivery string
		// mode is the mode of the file in the repository.
		mode     os.FileMode
		plant    func(t *testing.T, s *installStand)
		wantPerm os.FileMode
	}{
		{
			name:     "downloaded_file_is_stored",
			delivery: "remote",
			mode:     0o755,
			wantPerm: 0o644,
		},
		{
			name:     "local_file_is_copied_with_its_mode",
			delivery: "local",
			mode:     0o750,
			wantPerm: 0o750,
		},
		{
			name:     "downloaded_file_replaces_a_link_to_a_file_outside",
			delivery: "remote",
			mode:     0o644,
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, filepath.Join(s.serverDir, "server.jar")))
			},
			wantPerm: 0o644,
		},
		{
			name:     "downloaded_file_replaces_a_link_to_a_missing_file_outside",
			delivery: "remote",
			mode:     0o644,
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Symlink(filepath.Join(s.outside, "created"), filepath.Join(s.serverDir, "server.jar")))
			},
			wantPerm: 0o644,
		},
		{
			name:     "local_file_replaces_a_link_to_a_file_outside",
			delivery: "local",
			mode:     0o644,
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				require.NoError(t, os.Symlink(s.secret, filepath.Join(s.serverDir, "server.jar")))
			},
			wantPerm: 0o644,
		},
		{
			name:     "file_replaces_an_older_and_longer_one",
			delivery: "remote",
			mode:     0o644,
			plant: func(t *testing.T, s *installStand) {
				t.Helper()

				old := filepath.Join(s.serverDir, "server.jar")
				require.NoError(t, os.WriteFile(old, []byte("the jar of the previous version"), 0o600))
			},
			wantPerm: 0o644,
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				repository := t.TempDir()
				source := filepath.Join(repository, "server.jar")
				require.NoError(t, os.WriteFile(source, []byte("PK jar content"), 0o600))
				require.NoError(t, os.Chmod(source, tt.mode))

				stand := givenInstallStand(t)
				if tt.plant != nil {
					tt.plant(t, stand)
				}

				err := stand.install(kind, deliveries[tt.delivery](t, source))

				stand.assertOutsideUntouched(t)
				require.NoError(t, err)
				assertInstalledFile(t, filepath.Join(stand.serverDir, "server.jar"), "PK jar content", tt.wantPerm)
				assertInstalledFile(t, source, "PK jar content", tt.mode)
			})
		}
	}
}

func TestInstallator_ArchiveFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		archive  string
		wantFile string
	}{
		{name: "tar_gz", archive: "game.tar.gz", wantFile: "game_file_from_tar_gz"},
		{name: "tar_xz", archive: "game.tar.xz", wantFile: "game_file_from_tar_xz"},
		{name: "zip", archive: "game.zip", wantFile: "game_file_from_zip"},
		{name: "seven_zip", archive: "game.7z", wantFile: "game_file_from_7z"},
	}

	for _, tt := range tests {
		for deliveryName, delivery := range deliveries {
			t.Run(tt.name+"/"+deliveryName, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)

				err := stand.install("installer", delivery(t, localRepositoryFixture(t, tt.archive)))

				require.NoError(t, err)
				assertInstalledFile(t, filepath.Join(stand.serverDir, tt.wantFile), tt.wantFile+"\n", 0o644)

				entries, err := os.ReadDir(stand.serverDir)
				require.NoError(t, err)
				require.Len(t, entries, 1, "the archive itself is not left in the server directory")
			})
		}
	}
}

func TestInstallator_EmptyArchive(t *testing.T) {
	t.Parallel()

	archive := filepath.Join(t.TempDir(), "game.zip")

	file, err := os.Create(archive)
	require.NoError(t, err)
	require.NoError(t, zip.NewWriter(file).Close())
	require.NoError(t, file.Close())

	for deliveryName, delivery := range deliveries {
		t.Run(deliveryName, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallStand(t)

			err := stand.install("installer", delivery(t, archive))

			require.Error(t, err)
			assert.Contains(t, err.Error(), `failed to unpack "game.zip": the archive holds no files`)
			assert.Contains(t, stand.output.String(), "the archive holds no files")
		})
	}
}

func TestInstallator_BrokenArchive(t *testing.T) {
	t.Parallel()

	archive := filepath.Join(t.TempDir(), "game.zip")
	require.NoError(t, os.WriteFile(archive, []byte("<html>the page a mirror serves instead</html>"), 0o644))

	for deliveryName, delivery := range deliveries {
		t.Run(deliveryName, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallStand(t)

			err := stand.install("installer", delivery(t, archive))

			require.Error(t, err)
			assert.Contains(t, err.Error(), `failed to unpack "game.zip"`)
			assert.Contains(t, err.Error(), "failed to read zip archive")

			entries, err := os.ReadDir(stand.serverDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

// What the remote repository field accepts is a link to a file. The sources
// only the library that used to download understood are refused one by one,
// and the installation goes on to the next way of installing the game.
func TestInstallator_UnsupportedRemoteSource(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"ftp":            "ftp://files.example.com/game.tar.gz",
		"s3_bucket":      "s3::https://s3.amazonaws.com/bucket/game.tar.gz",
		"git_repository": "git::https://github.com/example/game.git",
		"directory":      "https://files.example.com/games/",
	}

	for name, source := range sources {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallStand(t)

			err := stand.install(
				"installer",
				remoteRule(source),
				localArchiveRule(localRepositoryFixture(t, "game.tar.gz")),
			)

			require.NoError(t, err)
			assert.Contains(t, stand.output.String(), "failed to download files")
			assert.Contains(t, stand.output.String(), "unsupported source, a direct http or https link to a file is expected")
			assertInstalledFile(t, filepath.Join(stand.serverDir, "game_file_from_tar_gz"), "game_file_from_tar_gz\n", 0o644)
		})
	}
}

func TestInstallator_UnsupportedRemoteSourceIsTheOnlyOne(t *testing.T) {
	t.Parallel()

	stand := givenInstallStand(t)

	err := stand.install("installer", remoteRule("ftp://files.example.com/game.tar.gz"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported source, a direct http or https link to a file is expected")
}

// An archive is not turned away for a symlink that leads out of the server
// directory: the link is left out, and the output of the installation says so.
func TestInstallator_ReportsSymlinksLeftOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		links       int
		wantLines   []string
		wantNoLines []string
	}{
		{
			name:  "every_link_is_named",
			links: 2,
			wantLines: []string{
				`Warning: symlink "lib/link0.so" -> "/usr/lib/link0.so" was not created: absolute symlink target`,
				`Warning: symlink "lib/link1.so" -> "/usr/lib/link1.so" was not created: absolute symlink target`,
			},
			wantNoLines: []string{"more symlinks were not created"},
		},
		{
			name:  "a_long_list_is_cut_short",
			links: 25,
			wantLines: []string{
				`Warning: symlink "lib/link0.so" -> "/usr/lib/link0.so" was not created: absolute symlink target`,
				`Warning: symlink "lib/link19.so" -> "/usr/lib/link19.so" was not created: absolute symlink target`,
				"... and 5 more symlinks were not created",
			},
			wantNoLines: []string{`"lib/link20.so"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entries := []archiveEntry{{name: "hlds_run", body: "#!/bin/sh", mode: 0o755}}
			for i := range tt.links {
				entries = append(entries, archiveEntry{
					name:     fmt.Sprintf("lib/link%d.so", i),
					linkname: fmt.Sprintf("/usr/lib/link%d.so", i),
				})
			}

			stand := givenInstallStand(t)

			err := stand.install("installer", localArchiveRule(givenTarGz(t, "game.tar.gz", entries...)))

			require.NoError(t, err)
			assertInstalledFile(t, filepath.Join(stand.serverDir, "hlds_run"), "#!/bin/sh", 0o755)
			assert.NoFileExists(t, filepath.Join(stand.serverDir, "lib", "link0.so"))

			for _, line := range tt.wantLines {
				assert.Contains(t, stand.output.String(), line)
			}

			for _, line := range tt.wantNoLines {
				assert.NotContains(t, stand.output.String(), line)
			}
		})
	}
}

// The server directory is created by the installation that finds none.
// steamcmd gets one nobody but the game server user looks into.
func TestInstallator_CreatesTheServerDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rule     func(t *testing.T) *installationRule
		wantPerm os.FileMode
	}{
		{
			name: "archive",
			rule: func(t *testing.T) *installationRule {
				t.Helper()

				return localArchiveRule(localRepositoryFixture(t, "game.tar.gz"))
			},
			wantPerm: 0o755,
		},
		{
			name: "steam",
			rule: func(*testing.T) *installationRule {
				return steamRule()
			},
			wantPerm: 0o750,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			steamCMD := filepath.Join(base, "steamcmd")
			require.NoError(t, os.Mkdir(steamCMD, 0o755))

			stand := givenInstallStand(t)
			stand.cfg = &config.Config{WorkPath: base, SteamCMDPath: steamCMD}
			stand.server = domain.NewServer(
				1, true, domain.ServerInstalled, false, "name",
				"759b875e-d910-11eb-aff7-d796d7fcf7ef", "759b875e",
				domain.Game{StartCode: "test"}, domain.GameMod{Name: "test"},
				"1.3.3.7", 1337, 1338, 1339, "paS$w0rD",
				"servers/759b875e", "gameap-user",
				"./run.sh", "", "", "", false, time.Now(),
				map[string]string{}, map[string]string{}, time.Now(), 0, 0,
			)

			err := stand.install("installer", tt.rule(t))

			require.NoError(t, err)

			info, err := os.Lstat(filepath.Join(base, "servers", "759b875e"))
			require.NoError(t, err)
			assert.Equal(t, os.ModeDir|tt.wantPerm, info.Mode())

			parent, err := os.Lstat(filepath.Join(base, "servers"))
			require.NoError(t, err)
			assert.Equal(t, os.ModeDir|0o755, parent.Mode(), "the directories on the way stay passable")
		})
	}
}

// The steamcmd directory is shared with the group of the game servers before
// steamcmd runs: group, mode and setgid bit of the whole tree are changed as
// root. It lies in the work directory, where a symlink can take its place.
func TestInstallator_SteamCMDDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// steamCMD prepares the steamcmd directory and returns the configured
		// path.
		steamCMD    func(t *testing.T, s *installStand) string
		wantError   string
		wantCommand bool
	}{
		{
			name: "directory_in_the_work_directory",
			steamCMD: func(t *testing.T, s *installStand) string {
				t.Helper()

				path := filepath.Join(s.cfg.WorkPath, "steamcmd")
				require.NoError(t, os.Mkdir(path, 0o755))

				return path
			},
			wantCommand: true,
		},
		{
			name: "directory_out_of_the_work_directory",
			steamCMD: func(t *testing.T, _ *installStand) string {
				t.Helper()

				return t.TempDir()
			},
			wantCommand: true,
		},
		{
			name: "missing_directory",
			steamCMD: func(_ *testing.T, s *installStand) string {
				return filepath.Join(s.cfg.WorkPath, "steamcmd")
			},
			wantError: "is not accessible",
		},
		{
			name: "symlink_out_of_the_work_directory_in_its_place",
			steamCMD: func(t *testing.T, s *installStand) string {
				t.Helper()

				path := filepath.Join(s.cfg.WorkPath, "steamcmd")
				require.NoError(t, os.Symlink(s.outside, path))

				return path
			},
			wantError: "symlinks that lead out of it are not followed",
		},
		{
			name: "relative_symlink_out_of_the_work_directory_in_its_place",
			steamCMD: func(t *testing.T, s *installStand) string {
				t.Helper()

				path := filepath.Join(s.cfg.WorkPath, "steamcmd")
				require.NoError(t, os.Symlink("../outside", path))

				return path
			},
			wantError: "symlinks that lead out of it are not followed",
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)
				stand.cfg.SteamCMDPath = tt.steamCMD(t, stand)

				err := stand.install(kind, steamRule())

				stand.assertOutsideUntouched(t)

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), "steamcmd directory "+stand.cfg.SteamCMDPath)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Empty(t, stand.executor.command, "steamcmd is not started")

					return
				}

				require.NoError(t, err)
				assert.Contains(t, stand.executor.command, "+app_update 90")
				assert.Equal(t, stand.cfg.SteamCMDPath, stand.executor.options.WorkDir)
			})
		}
	}
}

// A download is kept in the temporary directory until it is installed. Nothing
// of it stays there, whatever becomes of the installation.
//
// Not parallel: the test points the temporary directory of the process
// elsewhere.
func TestInstallator_DownloadLeavesNothingInTheTempDir(t *testing.T) {
	fixture := localRepositoryFixture(t, "game.tar.gz")

	tests := []struct {
		name      string
		handler   http.HandlerFunc
		wantError string
	}{
		{
			name: "installed_archive",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, fixture)
			},
		},
		{
			name: "broken_archive",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "<html>the page a mirror serves instead</html>")
			},
			wantError: "failed to unpack",
		},
		{
			name: "download_that_breaks_off",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "1048576")
				_, _ = io.WriteString(w, "only the beginning")
			},
			wantError: "failed to download files",
		},
		{
			name: "refused_download",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "forbidden", http.StatusForbidden)
			},
			wantError: `the server answered "403 Forbidden"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)

			repository := httptest.NewServer(tt.handler)
			defer repository.Close()

			stand := givenInstallStand(t)

			err := stand.install("installer", remoteRule(repository.URL+"/game.tar.gz"))

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
			} else {
				require.NoError(t, err)
			}

			entries, err := os.ReadDir(tempDir)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}
}

// The tests below need what the installation needs to do harm: root, and a
// game server user to hand files to. They run on a node that has the user of
// the server givenServer describes, and are skipped elsewhere.

type fileOwnership struct {
	uid  int
	gid  int
	mode os.FileMode
}

func ownershipOf(t *testing.T, path string) fileOwnership {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)

	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	return fileOwnership{uid: int(stat.Uid), gid: int(stat.Gid), mode: info.Mode()}
}

// requireRootAndTenant returns the ids of the game server user, or skips the
// test where it cannot run.
func requireRootAndTenant(t *testing.T, s *installStand) (uid, gid int) {
	t.Helper()

	if os.Geteuid() != 0 {
		t.Skip("changing the owner of files requires root")
	}

	tenant, err := user.Lookup(s.server.User())
	if err != nil {
		t.Skipf("the game server user %q is not there: %v", s.server.User(), err)
	}

	uid, err = strconv.Atoi(tenant.Uid)
	require.NoError(t, err)
	gid, err = strconv.Atoi(tenant.Gid)
	require.NoError(t, err)

	return uid, gid
}

// handOver gives the server directory to the tenant, as a first installation
// has done by the time a game server can leave anything in it.
func (s *installStand) handOver(t *testing.T, uid, gid int) {
	t.Helper()

	require.NoError(t, os.Chown(s.serverDir, uid, gid))
}

func TestInstallatorAsRoot_InstalledTreeBelongsToTheGameServerUser(t *testing.T) {
	t.Parallel()

	archive := givenTarGz(t, "game.tar.gz",
		archiveEntry{name: "cstrike/"},
		archiveEntry{name: "cstrike/server.cfg", body: "hostname"},
		archiveEntry{name: "hlds_run", body: "#!/bin/sh", mode: 0o755},
		archiveEntry{name: "libSDL2.so", linkname: "libSDL2-2.0.so.0"},
		archiveEntry{name: "libSDL2-2.0.so.0", body: "ELF", mode: 0o755},
	)

	for deliveryName, delivery := range deliveries {
		for kind := range installatorKinds {
			t.Run(deliveryName+"/"+kind, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)
				uid, gid := requireRootAndTenant(t, stand)

				err := stand.install(kind, delivery(t, archive))

				require.NoError(t, err)

				for _, name := range []string{".", "cstrike", "cstrike/server.cfg", "hlds_run", "libSDL2.so", "libSDL2-2.0.so.0"} {
					got := ownershipOf(t, filepath.Join(stand.serverDir, name))
					assert.Equal(t, uid, got.uid, name)
					assert.Equal(t, gid, got.gid, name)
				}

				assert.Zero(t, ownershipOf(t, stand.cfg.WorkPath).uid, "the work directory is not handed over")
			})
		}
	}
}

func TestInstallatorAsRoot_ArchiveFileMeetsASymlink(t *testing.T) {
	t.Parallel()

	for deliveryName, delivery := range deliveries {
		for kind := range installatorKinds {
			t.Run(deliveryName+"/"+kind, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)
				uid, gid := requireRootAndTenant(t, stand)
				stand.handOver(t, uid, gid)

				link := filepath.Join(stand.serverDir, "game_file_from_tar_gz")
				require.NoError(t, os.Symlink(stand.secret, link))
				require.NoError(t, os.Lchown(link, uid, gid))

				before := ownershipOf(t, stand.secret)

				err := stand.install(kind, delivery(t, localRepositoryFixture(t, "game.tar.gz")))

				require.NoError(t, err)
				stand.assertOutsideUntouched(t)
				assert.Equal(t, before, ownershipOf(t, stand.secret), "the file out of the work directory keeps its owner")
				assertInstalledFile(t, link, "game_file_from_tar_gz\n", 0o644)
				assert.Equal(t, uid, ownershipOf(t, link).uid)
			})
		}
	}
}

// A link the tenant keeps in the server directory is given to the tenant with
// the rest of the tree. What it leads to is not.
func TestInstallatorAsRoot_SymlinkInTheServerDirIsNotFollowed(t *testing.T) {
	t.Parallel()

	stand := givenInstallStand(t)
	uid, gid := requireRootAndTenant(t, stand)
	stand.handOver(t, uid, gid)

	require.NoError(t, os.Symlink(stand.secret, filepath.Join(stand.serverDir, "kept_link_to_file")))
	require.NoError(t, os.Symlink(stand.outside, filepath.Join(stand.serverDir, "kept_link_to_dir")))

	fileBefore := ownershipOf(t, stand.secret)
	dirBefore := ownershipOf(t, stand.outside)

	err := stand.install("updater", localArchiveRule(localRepositoryFixture(t, "game.tar.gz")))

	require.NoError(t, err)
	stand.assertOutsideUntouched(t)
	assert.Equal(t, fileBefore, ownershipOf(t, stand.secret))
	assert.Equal(t, dirBefore, ownershipOf(t, stand.outside))
	assert.Equal(t, uid, ownershipOf(t, filepath.Join(stand.serverDir, "kept_link_to_file")).uid)
	assert.Equal(t, uid, ownershipOf(t, filepath.Join(stand.serverDir, "kept_link_to_dir")).uid)
}

// With a symlink in place of the server directory every installation, steamcmd
// included, used to give the tree the link names to the game server user.
func TestInstallatorAsRoot_ServerDirReplacedByASymlink(t *testing.T) {
	t.Parallel()

	rules := map[string]func(t *testing.T) *installationRule{
		"local_archive": func(t *testing.T) *installationRule {
			t.Helper()

			return localArchiveRule(localRepositoryFixture(t, "game.tar.gz"))
		},
		"steam": func(*testing.T) *installationRule {
			return steamRule()
		},
	}

	for ruleName, rule := range rules {
		for kind := range installatorKinds {
			t.Run(ruleName+"/"+kind, func(t *testing.T) {
				t.Parallel()

				stand := givenInstallStand(t)
				requireRootAndTenant(t, stand)

				stand.cfg.SteamCMDPath = filepath.Join(stand.cfg.WorkPath, "steamcmd")
				require.NoError(t, os.Mkdir(stand.cfg.SteamCMDPath, 0o755))

				require.NoError(t, os.Remove(stand.serverDir))
				require.NoError(t, os.Symlink(stand.outside, stand.serverDir))

				fileBefore := ownershipOf(t, stand.secret)
				dirBefore := ownershipOf(t, stand.outside)

				err := stand.install(kind, rule(t))

				assert.Equal(t, fileBefore, ownershipOf(t, stand.secret), "the file the link leads to keeps its owner")
				assert.Equal(t, dirBefore, ownershipOf(t, stand.outside), "the directory the link leads to keeps its owner")
				stand.assertOutsideUntouched(t)
				require.Error(t, err)
				assert.Empty(t, stand.executor.command)
			})
		}
	}
}

// With a symlink in place of the steamcmd directory any steamcmd installation
// used to make the tree the link names writable for the group of the game
// servers, directory by directory and file by file.
func TestInstallatorAsRoot_SteamCMDDirReplacedByASymlink(t *testing.T) {
	t.Parallel()

	for kind := range installatorKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			stand := givenInstallStand(t)
			requireRootAndTenant(t, stand)

			stand.cfg.SteamCMDPath = filepath.Join(stand.cfg.WorkPath, "steamcmd")
			require.NoError(t, os.Symlink(stand.outside, stand.cfg.SteamCMDPath))

			fileBefore := ownershipOf(t, stand.secret)
			dirBefore := ownershipOf(t, stand.outside)

			err := stand.install(kind, steamRule())

			assert.Equal(t, fileBefore, ownershipOf(t, stand.secret), "the file the link leads to keeps its group and mode")
			assert.Equal(t, dirBefore, ownershipOf(t, stand.outside), "the directory the link leads to keeps its group and mode")
			stand.assertOutsideUntouched(t)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "symlinks that lead out of it are not followed")
			assert.Empty(t, stand.executor.command)
		})
	}
}

func TestInstallatorAsRoot_SteamCMDDirIsSharedWithTheGameServerGroup(t *testing.T) {
	t.Parallel()

	stand := givenInstallStand(t)
	uid, gid := requireRootAndTenant(t, stand)

	stand.cfg.SteamCMDPath = filepath.Join(stand.cfg.WorkPath, "steamcmd")
	require.NoError(t, os.MkdirAll(filepath.Join(stand.cfg.SteamCMDPath, "linux32"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stand.cfg.SteamCMDPath, "steamcmd.sh"), []byte("#!/bin/sh"), 0o755))
	require.NoError(t, os.Symlink(stand.secret, filepath.Join(stand.cfg.SteamCMDPath, "link")))

	fileBefore := ownershipOf(t, stand.secret)

	err := stand.install("installer", steamRule())

	require.NoError(t, err)
	stand.assertOutsideUntouched(t)
	assert.Equal(t, fileBefore, ownershipOf(t, stand.secret), "a link in the steamcmd directory is not followed")

	for _, name := range []string{".", "linux32"} {
		got := ownershipOf(t, filepath.Join(stand.cfg.SteamCMDPath, name))
		assert.Equal(t, gid, got.gid, name)
		assert.Zero(t, got.uid, "%s keeps its owner", name)
		assert.NotZero(t, got.mode&os.ModeSetgid, "%s has to carry the setgid bit", name)
		assert.Equal(t, os.FileMode(0o775), got.mode.Perm(), name)
	}

	script := ownershipOf(t, filepath.Join(stand.cfg.SteamCMDPath, "steamcmd.sh"))
	assert.Equal(t, gid, script.gid)
	assert.Equal(t, os.FileMode(0o775), script.mode.Perm())

	server := ownershipOf(t, stand.serverDir)
	assert.Equal(t, uid, server.uid, "steamcmd installs as the game server user into a directory of its own")
	assert.Equal(t, strconv.Itoa(uid), stand.executor.options.UID)
	assert.Equal(t, strconv.Itoa(gid), stand.executor.options.GID)
}
