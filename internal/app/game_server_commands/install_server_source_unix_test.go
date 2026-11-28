//go:build linux || darwin

package gameservercommands

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests of this file are about the two parameters a remote repository link
// may carry for the daemon: archive, which says whether the file is unpacked
// and as what, and checksum, which the file has to hash to.

// recordingRepository is a remote repository that remembers what was asked of
// it.
type recordingRepository struct {
	url string

	mu       sync.Mutex
	requests []string
}

func (r *recordingRepository) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.requests...)
}

// givenRecordingRepository serves the files of dir.
func givenRecordingRepository(t *testing.T, dir string) *recordingRepository {
	t.Helper()

	repository := &recordingRepository{}
	files := http.FileServer(http.Dir(dir))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repository.mu.Lock()
		repository.requests = append(repository.requests, r.URL.RequestURI())
		repository.mu.Unlock()

		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	repository.url = server.URL

	return repository
}

// serveFixtureAs puts a file of the local repository fixtures into a directory
// of its own under another name and returns the directory and the content.
func serveFixtureAs(t *testing.T, fixture, name string) (string, []byte) {
	t.Helper()

	content, err := os.ReadFile(localRepositoryFixture(t, fixture))
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0o644))

	return dir, content
}

func sha256Of(content []byte) string {
	sum := sha256.Sum256(content)

	return hex.EncodeToString(sum[:])
}

func TestInstallator_ArchiveParameter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// fixture is the file of the local repository that is served, and
		// servedAs the name the link gives it.
		fixture  string
		servedAs string
		query    string
		// wantFile is the file the archive holds. The file is expected to be
		// stored as it is served when there is none.
		wantFile string
	}{
		{
			name:     "format_of_a_file_whose_name_does_not_tell",
			fixture:  "game.tar.gz",
			servedAs: "download",
			query:    "?archive=tar.gz",
			wantFile: "game_file_from_tar_gz",
		},
		{
			name:     "short_name_of_the_format",
			fixture:  "game.tar.gz",
			servedAs: "download",
			query:    "?archive=tgz",
			wantFile: "game_file_from_tar_gz",
		},
		{
			name:     "tar_xz",
			fixture:  "game.tar.xz",
			servedAs: "latest",
			query:    "?archive=tar.xz",
			wantFile: "game_file_from_tar_xz",
		},
		{
			name:     "zip",
			fixture:  "game.zip",
			servedAs: "latest",
			query:    "?archive=zip",
			wantFile: "game_file_from_zip",
		},
		{
			name:     "seven_zip",
			fixture:  "game.7z",
			servedAs: "latest",
			query:    "?archive=7z",
			wantFile: "game_file_from_7z",
		},
		{
			name:     "format_in_upper_case",
			fixture:  "game.zip",
			servedAs: "latest",
			query:    "?archive=ZIP",
			wantFile: "game_file_from_zip",
		},
		{
			name:     "among_the_parameters_of_the_server",
			fixture:  "game.zip",
			servedAs: "latest",
			query:    "?token=abc&archive=zip&v=2",
			wantFile: "game_file_from_zip",
		},
		{
			name:     "file_that_is_an_archive_only_to_who_looks_inside",
			fixture:  "game.zip",
			servedAs: "server.jar",
			query:    "?archive=zip",
			wantFile: "game_file_from_zip",
		},
		{
			name:     "archive_whose_format_the_content_tells",
			fixture:  "game.tar.gz",
			servedAs: "download",
			query:    "?archive=true",
			wantFile: "game_file_from_tar_gz",
		},
		{
			name:     "archive_whose_name_tells_another_format_than_the_content",
			fixture:  "game.tar.gz",
			servedAs: "game.zip",
			query:    "?archive=true",
			wantFile: "game_file_from_tar_gz",
		},
		{
			name:     "archive_kept_as_it_is",
			fixture:  "game.zip",
			servedAs: "game.zip",
			query:    "?archive=false",
		},
		{
			name:     "archive_kept_as_it_is_among_the_parameters_of_the_server",
			fixture:  "game.tar.gz",
			servedAs: "game.tar.gz",
			query:    "?token=abc&archive=0",
		},
		{
			name:     "empty_parameter_leaves_it_to_the_name_of_an_archive",
			fixture:  "game.zip",
			servedAs: "game.zip",
			query:    "?archive=",
			wantFile: "game_file_from_zip",
		},
		{
			name:     "empty_parameter_leaves_it_to_the_name_of_a_file",
			fixture:  "game.zip",
			servedAs: "server.jar",
			query:    "?archive=",
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				dir, content := serveFixtureAs(t, tt.fixture, tt.servedAs)
				stand := givenInstallStand(t)

				err := stand.install(kind, remoteRule(givenRemoteRepository(t, dir)+"/"+tt.servedAs+tt.query))

				require.NoError(t, err, stand.output.String())

				if tt.wantFile == "" {
					assertInstalledFile(t, filepath.Join(stand.serverDir, tt.servedAs), string(content), 0o644)
					assert.Equal(t, []string{tt.servedAs}, entryNames(t, stand.serverDir))

					return
				}

				assertInstalledFile(t, filepath.Join(stand.serverDir, tt.wantFile), tt.wantFile+"\n", 0o644)
				assert.Equal(t, []string{tt.wantFile}, entryNames(t, stand.serverDir), "the archive itself is not left")
			})
		}
	}
}

// A source that asks for what cannot be done fails like any other, and the
// installation goes on to the next one.
func TestInstallator_ArchiveParameterThatCannotBeFollowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// fixture is the file of the local repository that is served. A text
		// file is served when there is none.
		fixture  string
		servedAs string
		query    string
		// wantRequests tells whether the file is downloaded to find out.
		wantRequests int
		wantOutput   []string
	}{
		{
			name:     "unknown_format",
			fixture:  "game.zip",
			servedAs: "game.zip",
			query:    "?archive=exe",
			wantOutput: []string{
				`failed to download files: unsupported archive parameter "exe" of the source: ` +
					`true, false or a format is expected, one of tar.gz, tar.bz2, tar.xz, tar.zst, tgz,`,
			},
		},
		{
			name:     "format_with_its_dot",
			fixture:  "game.zip",
			servedAs: "game.zip",
			query:    "?archive=.zip",
			wantOutput: []string{
				`unsupported archive parameter ".zip" of the source`,
			},
		},
		{
			name:         "format_the_file_is_not_of",
			fixture:      "game.tar.gz",
			servedAs:     "download",
			query:        "?archive=zip",
			wantRequests: 1,
			wantOutput:   []string{`failed to unpack "download"`, "failed to read zip archive"},
		},
		{
			name:         "file_that_is_no_archive",
			servedAs:     "readme",
			query:        "?archive=true",
			wantRequests: 1,
			wantOutput:   []string{`failed to unpack "readme"`, `cannot detect the format of archive "readme"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.fixture != "" {
				dir, _ = serveFixtureAs(t, tt.fixture, tt.servedAs)
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tt.servedAs), []byte("no archive at all\n"), 0o644))
			}

			repository := givenRecordingRepository(t, dir)
			stand := givenInstallStand(t)

			err := stand.install(
				"installer",
				remoteRule(repository.url+"/"+tt.servedAs+tt.query),
				localArchiveRule(localRepositoryFixture(t, "game.tar.xz")),
			)

			require.NoError(t, err)

			for _, want := range tt.wantOutput {
				assert.Contains(t, stand.output.String(), want)
			}

			assert.Len(t, repository.requested(), tt.wantRequests)
			assert.Equal(t, []string{"game_file_from_tar_xz"}, entryNames(t, stand.serverDir))
		})
	}
}

func TestInstallator_Checksum(t *testing.T) {
	t.Parallel()

	fixture, err := os.ReadFile(localRepositoryFixture(t, "game.tar.gz"))
	require.NoError(t, err)

	sum := sha256Of(fixture)

	tests := []struct {
		name string
		// sums is the content of the checksum file the repository has, if any.
		sums  string
		query func(repository string) string
	}{
		{
			name:  "sum",
			query: func(string) string { return "?checksum=" + sum },
		},
		{
			name:  "sum_with_its_type",
			query: func(string) string { return "?checksum=sha256:" + sum },
		},
		{
			name:  "sum_among_the_parameters_of_the_server",
			query: func(string) string { return "?token=abc&checksum=sha256:" + sum + "&v=2" },
		},
		{
			name:  "checksum_file",
			sums:  sha256Of([]byte("another file")) + "  game.zip\n" + sum + "  game.tar.gz\n",
			query: func(repository string) string { return "?checksum=file:" + repository + "/SHA256SUMS" },
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				dir, _ := serveFixtureAs(t, "game.tar.gz", "game.tar.gz")
				if tt.sums != "" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(tt.sums), 0o644))
				}

				repository := givenRemoteRepository(t, dir)
				stand := givenInstallStand(t)

				err := stand.install(kind, remoteRule(repository+"/game.tar.gz"+tt.query(repository)))

				require.NoError(t, err, stand.output.String())
				assertInstalledFile(
					t, filepath.Join(stand.serverDir, "game_file_from_tar_gz"), "game_file_from_tar_gz\n", 0o644,
				)
				assert.Equal(t, []string{"game_file_from_tar_gz"}, entryNames(t, stand.serverDir))
			})
		}
	}
}

// A mirror that serves another file than the one the link was written for is
// one more source that failed: nothing of its file reaches the server
// directory, and the next source installs the game.
func TestInstallator_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	expected, err := os.ReadFile(localRepositoryFixture(t, "game.tar.gz"))
	require.NoError(t, err)

	sum := sha256Of(expected)

	tests := []struct {
		name string
		// served is the fixture the mirror has under the name of the archive.
		served string
		// sums is the content of the checksum file the mirror has, if any.
		sums       string
		query      func(mirror string) string
		wantOutput string
	}{
		{
			name:       "another_archive",
			served:     "game_mod.tar.gz",
			query:      func(string) string { return "?checksum=sha256:" + sum },
			wantOutput: "sha256 " + sum + " is expected and the file has ",
		},
		{
			name:       "plain_file_that_would_be_kept_as_it_is",
			served:     "game_mod.tar.gz",
			query:      func(string) string { return "?archive=false&checksum=" + sum },
			wantOutput: "checksum mismatch",
		},
		{
			name:       "another_archive_than_the_checksum_file_lists",
			served:     "game_mod.tar.gz",
			sums:       sum + "  game.tar.gz\n",
			query:      func(mirror string) string { return "?checksum=file:" + mirror + "/SHA256SUMS" },
			wantOutput: "checksum mismatch",
		},
		{
			name:       "checksum_file_that_does_not_list_the_archive",
			served:     "game.tar.gz",
			sums:       sum + "  game.zip\n",
			query:      func(mirror string) string { return "?checksum=file:" + mirror + "/SHA256SUMS" },
			wantOutput: `/SHA256SUMS" has no checksum for "game.tar.gz"`,
		},
		{
			name:       "checksum_file_that_is_not_there",
			served:     "game.tar.gz",
			query:      func(mirror string) string { return "?checksum=file:" + mirror + "/SHA256SUMS" },
			wantOutput: `failed to get the checksum file`,
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				dir, _ := serveFixtureAs(t, tt.served, "game.tar.gz")
				if tt.sums != "" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(tt.sums), 0o644))
				}

				mirror := givenRemoteRepository(t, dir)
				source := mirror + "/game.tar.gz" + tt.query(mirror)

				alone := givenInstallStand(t)

				err := alone.install(kind, remoteRule(source))

				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to download files")
				assert.Contains(t, err.Error(), tt.wantOutput)
				assert.Contains(t, alone.output.String(), tt.wantOutput)
				assert.Empty(t, entryNames(t, alone.serverDir), "nothing of a file that is turned away is installed")

				followed := givenInstallStand(t)

				err = followed.install(kind, remoteRule(source), localArchiveRule(localRepositoryFixture(t, "game.tar.gz")))

				require.NoError(t, err)
				assert.Contains(t, followed.output.String(), tt.wantOutput)
				assert.Equal(t, []string{"game_file_from_tar_gz"}, entryNames(t, followed.serverDir))
			})
		}
	}
}

func TestInstallator_InvalidChecksumIsFoundBeforeTheDownload(t *testing.T) {
	t.Parallel()

	dir, _ := serveFixtureAs(t, "game.tar.gz", "game.tar.gz")
	repository := givenRecordingRepository(t, dir)
	stand := givenInstallStand(t)

	err := stand.install("installer", remoteRule(repository.url+"/game.tar.gz?checksum=sha256:abcdef"))

	require.Error(t, err)
	assert.Contains(
		t, err.Error(),
		"failed to download files: invalid checksum parameter of the source: a sha256 checksum has 64 hex digits, 6 are given",
	)
	assert.Empty(t, repository.requested())
	assert.Empty(t, entryNames(t, stand.serverDir))
}

func TestInstallator_FileWithAChecksumLeavesNothingInTheTempDir(t *testing.T) {
	dir, content := serveFixtureAs(t, "game.tar.gz", "game.tar.gz")
	repository := givenRemoteRepository(t, dir)

	tests := []struct {
		name      string
		sum       string
		wantError string
	}{
		{name: "verified", sum: sha256Of(content)},
		{name: "turned_away", sum: sha256Of([]byte("another file")), wantError: "checksum mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("TMPDIR", tempDir)

			stand := givenInstallStand(t)

			err := stand.install("installer", remoteRule(repository+"/game.tar.gz?checksum="+tt.sum))

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

// The output of an installation is read by whoever looks at the task, and the
// link of a repository is what gives access to it. The output names the source
// without the password of the link and without its query, where a signed link
// keeps its token and where the link to a checksum file stands. The errors of a
// download that failed go the same way.
func TestInstallator_OutputShowsTheSourceWithoutWhatGivesAccessToIt(t *testing.T) {
	t.Parallel()

	archive, err := os.ReadFile(localRepositoryFixture(t, "game.tar.gz"))
	require.NoError(t, err)

	tests := []struct {
		name string
		// file is what the link names, and sums the content of the checksum
		// file the mirror has, if any.
		file       string
		sums       string
		wantError  bool
		wantOutput string
	}{
		{
			name: "installed",
			file: "game.tar.gz",
			sums: sha256Of(archive) + "  game.tar.gz\n",
		},
		{
			name:       "file_that_is_not_there",
			file:       "missing.tar.gz",
			sums:       sha256Of(archive) + "  missing.tar.gz\n",
			wantError:  true,
			wantOutput: `/missing.tar.gz": the server answered "404 Not Found"`,
		},
		{
			name:       "file_that_does_not_match_its_checksum",
			file:       "game.tar.gz",
			sums:       sha256Of([]byte("another file")) + "  game.tar.gz\n",
			wantError:  true,
			wantOutput: `/game.tar.gz": sha256 `,
		},
		{
			name:       "checksum_file_that_does_not_list_the_file",
			file:       "game.tar.gz",
			sums:       sha256Of(archive) + "  game.zip\n",
			wantError:  true,
			wantOutput: `/SHA256SUMS" has no checksum for "game.tar.gz"`,
		},
		{
			name:       "checksum_file_that_is_not_there",
			file:       "game.tar.gz",
			wantError:  true,
			wantOutput: `/SHA256SUMS": the server answered "404 Not Found"`,
		},
	}

	for _, tt := range tests {
		for kind := range installatorKinds {
			t.Run(tt.name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				dir, _ := serveFixtureAs(t, "game.tar.gz", "game.tar.gz")
				if tt.sums != "" {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(tt.sums), 0o644))
				}

				mirror := givenRemoteRepository(t, dir)
				shown := strings.Replace(mirror, "http://", "http://repo:xxxxx@", 1) + "/" + tt.file

				source := strings.Replace(mirror, "http://", "http://repo:secret@", 1) + "/" + tt.file +
					"?token=secret-token" +
					"&checksum=file:" + strings.Replace(mirror, "http://", "http://sums:secret@", 1) +
					"/SHA256SUMS?key=secret-key"

				stand := givenInstallStand(t)

				err := stand.install(kind, remoteRule(source))

				output := stand.output.String()

				assert.Contains(t, output, "Downloading and unpacking from "+shown+" to ")
				assert.NotContains(t, output, "secret")
				assert.NotContains(t, output, "token")
				assert.NotContains(t, output, "checksum=")

				if !tt.wantError {
					require.NoError(t, err)
					assert.Equal(t, []string{"game_file_from_tar_gz"}, entryNames(t, stand.serverDir))

					return
				}

				require.Error(t, err)
				assert.NotContains(t, err.Error(), "secret")
				assert.Contains(t, output, tt.wantOutput)
			})
		}
	}
}
