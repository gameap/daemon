package fetch

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fileContent = "game archive content"
	otherSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
)

func md5Of(content string) string {
	sum := md5.Sum([]byte(content))

	return hex.EncodeToString(sum[:])
}

func sha1Of(content string) string {
	sum := sha1.Sum([]byte(content))

	return hex.EncodeToString(sum[:])
}

func sha256Of(content string) string {
	sum := sha256.Sum256([]byte(content))

	return hex.EncodeToString(sum[:])
}

func sha512Of(content string) string {
	sum := sha512.Sum512([]byte(content))

	return hex.EncodeToString(sum[:])
}

// repository serves a set of files and remembers what was asked of it.
type repository struct {
	mu       sync.Mutex
	files    map[string]string
	requests []string
}

func (r *repository) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req.URL.RequestURI())
	content, ok := r.files[req.URL.Path]
	r.mu.Unlock()

	if !ok {
		http.NotFound(w, req)

		return
	}

	_, _ = io.WriteString(w, content)
}

func (r *repository) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.requests...)
}

func givenRepository(t *testing.T, files map[string]string) (*repository, string) {
	t.Helper()

	repo := &repository{files: files}

	return repo, newServer(t, repo.ServeHTTP).URL
}

func TestParse_Archive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "source_without_one", source: "https://example.com/game.zip?token=abc", want: ""},
		{name: "format", source: "https://example.com/download?archive=zip", want: "zip"},
		{name: "format_among_other_parameters", source: "https://example.com/download?id=5&archive=tar.gz&token=abc", want: "tar.gz"},
		{name: "not_an_archive", source: "https://example.com/game.zip?archive=false", want: "false"},
		{name: "escaped_value", source: "https://example.com/download?archive=tar%2Egz", want: "tar.gz"},
		{name: "empty_value", source: "https://example.com/game.zip?archive=", want: ""},
		{name: "name_without_a_value", source: "https://example.com/game.zip?archive", want: ""},
		{name: "another_parameter_that_ends_the_same", source: "https://example.com/game.zip?myarchive=zip", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := Parse(tt.source)

			require.NoError(t, err)
			assert.Equal(t, tt.want, source.Archive())
		})
	}
}

func TestParse_InvalidParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantError string
	}{
		{
			name:      "checksum_that_is_not_hex",
			source:    "https://example.com/game.zip?checksum=not-a-checksum",
			wantError: "invalid checksum parameter of the source: failed to decode the checksum, hex digits are expected",
		},
		{
			name:      "checksum_of_no_known_length",
			source:    "https://example.com/game.zip?checksum=abcdef",
			wantError: "a checksum of 6 hex digits is of no known type: md5 has 32, sha1 40, sha256 64 and sha512 128",
		},
		{
			name:      "checksum_of_another_length_than_its_type_has",
			source:    "https://example.com/game.zip?checksum=sha256:" + md5Of(fileContent),
			wantError: "a sha256 checksum has 64 hex digits, 32 are given",
		},
		{
			name:      "typed_checksum_that_is_not_hex",
			source:    "https://example.com/game.zip?checksum=md5:zz",
			wantError: "failed to decode the checksum, hex digits are expected",
		},
		{
			name:      "unsupported_checksum_type",
			source:    "https://example.com/game.zip?checksum=crc32:cbf43926",
			wantError: `unsupported checksum type "crc32": md5, sha1, sha256 or sha512 is expected, or file for the link to a checksum file`,
		},
		{
			name:      "link_to_a_checksum_file_given_without_its_prefix",
			source:    "https://example.com/game.zip?checksum=https://user:secret@example.com/SHA256SUMS",
			wantError: `unsupported checksum type "https"`,
		},
		{
			name:      "checksum_file_on_the_disk_of_the_node",
			source:    "https://example.com/game.zip?checksum=file:/srv/repository/SHA256SUMS",
			wantError: `"/srv/repository/SHA256SUMS": unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "checksum_file_named_relative_to_the_source",
			source:    "https://example.com/game.zip?checksum=file:SHA256SUMS",
			wantError: "unsupported source, a direct http or https link to a file is expected",
		},
		{
			name:      "checksum_file_on_an_ftp_server",
			source:    "https://example.com/game.zip?checksum=file:ftp://user:secret@example.com/SHA256SUMS",
			wantError: `"ftp://user:xxxxx@example.com/SHA256SUMS": unsupported source`,
		},
		{
			name:      "link_to_the_checksum_file_cannot_be_parsed",
			source:    "https://example.com/game.zip?checksum=file:https://user:secret@example.com:port/SHA256SUMS",
			wantError: "invalid checksum parameter of the source: failed to parse the link to the checksum file: it is not a valid URL",
		},
		{
			name:      "wrong_escape_in_the_checksum",
			source:    "https://example.com/game.zip?checksum=file:https://user:secret%zz@example.com/SHA256SUMS",
			wantError: "the checksum parameter of the source has a percent sign that is not followed by two hex digits",
		},
		{
			name:      "two_checksums",
			source:    "https://example.com/game.zip?checksum=" + md5Of(fileContent) + "&checksum=" + sha1Of(fileContent),
			wantError: "the source has more than one checksum parameter",
		},
		{
			name:      "two_archive_parameters",
			source:    "https://example.com/game.zip?archive=false&token=abc&archive=zip",
			wantError: "the source has more than one archive parameter",
		},
		{
			name:      "parameters_of_an_unsupported_source_are_not_repeated",
			source:    "ftp://example.com/game.zip?checksum=file:https://user:secret@example.com/SHA256SUMS&token=abc",
			wantError: `"ftp://example.com/game.zip": unsupported source`,
		},
		{
			name:      "parameters_of_a_source_that_names_no_file_are_not_repeated",
			source:    "https://example.com/?checksum=file:https://user:secret@example.com/SHA256SUMS",
			wantError: `"https://example.com/" names no file`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := Parse(tt.source)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.NotContains(t, err.Error(), "secret")
			assert.Nil(t, source)
		})
	}
}

// The parameters of the daemon are not the server's to see, and the rest of
// the query is the server's alone: a signed link is requested the way it is
// written.
func TestDownload_ParametersOfTheDaemonAreNotSent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		query     string
		wantQuery string
	}{
		{
			name:      "between_the_parameters_of_the_server",
			query:     "?X-Signature=a%2Fb+c%3D&checksum=" + sha256Of(fileContent) + "&b=2&archive=zip&a=1&flag",
			wantQuery: "?X-Signature=a%2Fb+c%3D&b=2&a=1&flag",
		},
		{
			name:      "the_only_parameters",
			query:     "?archive=false&checksum=md5:" + md5Of(fileContent),
			wantQuery: "",
		},
		{
			name:      "escaped_name",
			query:     "?%63hecksum=" + sha1Of(fileContent) + "&token=abc",
			wantQuery: "?token=abc",
		},
		{
			name:      "none",
			query:     "?token=abc&&x=%zz",
			wantQuery: "?token=abc&&x=%zz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, address := givenRepository(t, map[string]string{"/repo/game.zip": fileContent})

			var dst bytes.Buffer
			tracker := &recordingTracker{}

			_, err := download(context.Background(), address+"/repo/game.zip"+tt.query, &dst, tracker)

			require.NoError(t, err)
			assert.Equal(t, fileContent, dst.String())
			assert.Equal(t, []string{"/repo/game.zip" + tt.wantQuery}, repo.requested())
			assert.Equal(t, address+"/repo/game.zip", tracker.src, "no query goes into the progress output")
		})
	}
}

func TestDownload_Checksum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		checksum string
	}{
		{name: "md5_told_by_its_length", checksum: md5Of(fileContent)},
		{name: "sha1_told_by_its_length", checksum: sha1Of(fileContent)},
		{name: "sha256_told_by_its_length", checksum: sha256Of(fileContent)},
		{name: "sha512_told_by_its_length", checksum: sha512Of(fileContent)},
		{name: "md5", checksum: "md5:" + md5Of(fileContent)},
		{name: "sha1", checksum: "sha1:" + sha1Of(fileContent)},
		{name: "sha256", checksum: "sha256:" + sha256Of(fileContent)},
		{name: "sha512", checksum: "sha512:" + sha512Of(fileContent)},
		{name: "type_in_upper_case", checksum: "SHA256:" + sha256Of(fileContent)},
		{name: "sum_in_upper_case", checksum: strings.ToUpper(sha256Of(fileContent))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, address := givenRepository(t, map[string]string{"/game.zip": fileContent})

			var dst bytes.Buffer

			written, err := download(context.Background(), address+"/game.zip?checksum="+tt.checksum, &dst, nil)

			require.NoError(t, err)
			assert.Equal(t, int64(len(fileContent)), written)
			assert.Equal(t, fileContent, dst.String())
		})
	}
}

func TestDownload_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		checksum  string
		wantError string
	}{
		{
			name:      "sha256",
			checksum:  otherSHA256,
			wantError: "sha256 " + otherSHA256 + " is expected and the file has " + sha256Of(fileContent),
		},
		{
			name:      "md5",
			checksum:  "md5:" + md5Of("another file"),
			wantError: "md5 " + md5Of("another file") + " is expected and the file has " + md5Of(fileContent),
		},
		{
			name:      "sum_of_another_type_than_the_one_named",
			checksum:  "sha1:" + sha1Of("another file"),
			wantError: "sha1 " + sha1Of("another file") + " is expected and the file has " + sha1Of(fileContent),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, address := givenRepository(t, map[string]string{"/game.zip": fileContent})
			source := strings.Replace(address, "http://", "http://repo:secret@", 1) + "/game.zip?checksum=" + tt.checksum

			var dst bytes.Buffer
			tracker := &recordingTracker{}

			written, err := download(context.Background(), source, &dst, tracker)

			require.ErrorIs(t, err, ErrChecksumMismatch)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Contains(t, err.Error(), "/game.zip")
			assert.NotContains(t, err.Error(), "secret")
			assert.Equal(t, int64(len(fileContent)), written, "what arrived is the caller's to throw away")
			assert.True(t, tracker.closed)
		})
	}
}

func TestDownload_ChecksumFile(t *testing.T) {
	t.Parallel()

	sum := sha256Of(fileContent)

	tests := []struct {
		name string
		// file is the path of the downloaded file, "/repo/game.zip" when empty.
		file string
		// list is the path of the checksum file, "/repo/SHA256SUMS" when empty.
		list      string
		content   string
		wantError string
		// wantMismatch tells that the file is downloaded and turned away.
		wantMismatch bool
	}{
		{
			name:    "gnu_text_mode",
			content: sum + "  game.zip\n",
		},
		{
			name:    "gnu_binary_mode",
			content: sum + " *game.zip\n",
		},
		{
			name:    "gnu_without_the_last_newline",
			content: sum + "  game.zip",
		},
		{
			name:    "bsd",
			content: "SHA256 (game.zip) = " + sum + "\n",
		},
		{
			name:    "bsd_md5",
			content: "MD5 (game.zip) = " + md5Of(fileContent) + "\n",
		},
		{
			name:    "sum_alone",
			list:    "/repo/game.zip.sha256",
			content: sum + "\n",
		},
		{
			name:    "md5_sums",
			list:    "/repo/MD5SUMS",
			content: md5Of(fileContent) + "  game.zip\n",
		},
		{
			name:    "sha512_sums",
			content: sha512Of(fileContent) + "  game.zip\n",
		},
		{
			name: "among_other_files",
			content: otherSHA256 + "  game.tar.gz\n" +
				otherSHA256 + "  other/game.zip\n" +
				otherSHA256 + "  game.zip.sig\n" +
				sum + "  game.zip\n" +
				otherSHA256 + "  zz_last.zip\n",
		},
		{
			name: "among_lines_that_are_not_checksums",
			content: "# checksums of the release\n" +
				"\n" +
				"-----BEGIN PGP SIGNED MESSAGE-----\n" +
				"Hash: SHA256\n" +
				"CRC32 (game.zip) = cbf43926\n" +
				sum + "  game.zip\n",
		},
		{
			name:    "windows_line_endings",
			content: otherSHA256 + "  game.tar.gz\r\n" + sum + "  game.zip\r\n",
		},
		{
			name:    "tab_between_the_sum_and_the_name",
			content: sum + "\tgame.zip\n",
		},
		{
			name:    "name_led_by_the_current_directory",
			content: sum + "  ./game.zip\n",
		},
		{
			name:    "path_from_the_directory_of_the_checksum_file",
			file:    "/repo/linux/game.zip",
			content: otherSHA256 + "  windows/game.zip\n" + sum + "  linux/game.zip\n",
		},
		{
			name:    "path_from_the_directory_of_the_checksum_file_in_the_root",
			list:    "/SHA256SUMS",
			content: sum + "  ./repo/game.zip\n",
		},
		{
			name:    "whole_path",
			list:    "/checksums/SHA256SUMS",
			content: sum + "  /repo/game.zip\n",
		},
		{
			name:    "name_with_a_space",
			file:    "/repo/my game.zip",
			content: otherSHA256 + "  my game.tar.gz\n" + sum + "  my game.zip\n",
		},
		{
			name:    "bsd_name_with_a_space_and_brackets",
			file:    "/repo/game (linux) = final.zip",
			content: "SHA256 (game (linux) = final.zip) = " + sum + "\n",
		},
		{
			name:      "file_is_not_listed",
			content:   otherSHA256 + "  game.tar.gz\n" + sum + "  other/game.zip\n",
			wantError: `/repo/SHA256SUMS" has no checksum for "game.zip"`,
		},
		{
			name:      "file_of_the_same_name_in_another_directory",
			file:      "/repo/linux/game.zip",
			content:   sum + "  windows/game.zip\n",
			wantError: `has no checksum for "game.zip"`,
		},
		{
			name:      "empty_checksum_file",
			content:   "",
			wantError: `has no checksum for "game.zip"`,
		},
		{
			name:      "page_served_in_place_of_the_checksum_file",
			content:   "<html><body>sign in to see the checksums of game.zip</body></html>\n",
			wantError: `has no checksum for "game.zip"`,
		},
		{
			name:         "sum_of_another_file",
			content:      sha256Of("another file") + "  game.zip\n",
			wantMismatch: true,
		},
		{
			name:         "first_of_two_lines_about_the_file_counts",
			content:      otherSHA256 + "  game.zip\n" + sum + "  game.zip\n",
			wantMismatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := tt.file
			if file == "" {
				file = "/repo/game.zip"
			}

			list := tt.list
			if list == "" {
				list = "/repo/SHA256SUMS"
			}

			repo, address := givenRepository(t, map[string]string{file: fileContent, list: tt.content})

			var dst bytes.Buffer

			source := address + strings.ReplaceAll(file, " ", "%20") + "?checksum=file:" + address + list
			_, err := download(context.Background(), source, &dst, nil)

			switch {
			case tt.wantError != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Equal(t, []string{list}, repo.requested(), "a file that cannot be verified is not downloaded")
				assert.Zero(t, dst.Len())
			case tt.wantMismatch:
				require.ErrorIs(t, err, ErrChecksumMismatch)
			default:
				require.NoError(t, err)
				assert.Equal(t, fileContent, dst.String())
				assert.Len(t, repo.requested(), 2)
				assert.Equal(t, list, repo.requested()[0], "the checksum file is asked for before the file")
			}
		})
	}
}

func TestDownload_ChecksumFileIsNotThere(t *testing.T) {
	t.Parallel()

	repo, address := givenRepository(t, map[string]string{"/game.zip": fileContent})
	list := strings.Replace(address, "http://", "http://repo:secret@", 1) + "/SHA256SUMS"

	var dst bytes.Buffer
	tracker := &recordingTracker{}

	written, err := download(context.Background(), address+"/game.zip?checksum=file:"+list, &dst, tracker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get the checksum file")
	assert.Contains(t, err.Error(), `/SHA256SUMS": the server answered "404 Not Found"`)
	assert.NotContains(t, err.Error(), "secret")
	assert.Zero(t, written)
	assert.Zero(t, dst.Len())
	assert.Zero(t, tracker.calls)
	assert.Equal(t, []string{"/SHA256SUMS"}, repo.requested())
}

// The link to a checksum file is requested the way it is written: its query
// and its credentials are its own, whatever the source has for the file.
func TestDownload_ChecksumFileIsRequestedAsItIsWritten(t *testing.T) {
	t.Parallel()

	var (
		mu                sync.Mutex
		listUser, listKey string
		listQuery         string
		fileUser          string
	)

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if r.URL.Path == "/sums/SHA256SUMS" {
			listUser, listKey, _ = r.BasicAuth()
			listQuery = r.URL.RawQuery

			_, _ = io.WriteString(w, sha256Of(fileContent)+"  game.zip\n")

			return
		}

		fileUser, _, _ = r.BasicAuth()

		_, _ = io.WriteString(w, fileContent)
	})

	list := strings.Replace(server.URL, "http://", "http://sums:secret@", 1) + "/sums/SHA256SUMS?archive=false%26v=2"

	var dst bytes.Buffer

	_, err := download(context.Background(), server.URL+"/repo/game.zip?checksum=file:"+list, &dst, nil)

	require.NoError(t, err)
	assert.Equal(t, fileContent, dst.String())

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, "sums", listUser)
	assert.Equal(t, "secret", listKey)
	assert.Equal(t, "archive=false&v=2", listQuery)
	assert.Empty(t, fileUser, "the credentials of the checksum file are not sent for the file")
}

func TestDownload_ChecksumFileIsTooLarge(t *testing.T) {
	t.Parallel()

	line := otherSHA256 + "  some_other_file.zip\n"
	list := strings.Repeat(line, maxChecksumFileSize/len(line)+1) + sha256Of(fileContent) + "  game.zip\n"

	repo, address := givenRepository(t, map[string]string{"/game.zip": fileContent, "/SHA256SUMS": list})

	var dst bytes.Buffer

	_, err := download(context.Background(), address+"/game.zip?checksum=file:"+address+"/SHA256SUMS", &dst, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `/SHA256SUMS" is larger than the 4194304 bytes that are read of one`)
	assert.Equal(t, []string{"/SHA256SUMS"}, repo.requested())
}

func TestDownload_CanceledWhileTheChecksumFileIsAskedFor(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	released := make(chan struct{})
	t.Cleanup(func() { close(released) })

	server := newServer(t, func(_ http.ResponseWriter, r *http.Request) {
		cancel()

		select {
		case <-r.Context().Done():
		case <-released:
		}
	})

	var dst bytes.Buffer

	_, err := download(ctx, server.URL+"/game.zip?checksum=file:"+server.URL+"/SHA256SUMS", &dst, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, dst.Len())
}
