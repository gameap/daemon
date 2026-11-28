package fetch

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTracker remembers what a download told it and whether the stream it
// handed out was closed.
type recordingTracker struct {
	mu        sync.Mutex
	calls     int
	src       string
	current   int64
	total     int64
	bytesRead int64
	closed    bool
}

func (r *recordingTracker) TrackProgress(src string, currentSize, totalSize int64, stream io.ReadCloser) io.ReadCloser {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++
	r.src = src
	r.current = currentSize
	r.total = totalSize

	return &recordedStream{ReadCloser: stream, tracker: r}
}

type recordedStream struct {
	io.ReadCloser
	tracker *recordingTracker
}

func (s *recordedStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)

	s.tracker.mu.Lock()
	s.tracker.bytesRead += int64(n)
	s.tracker.mu.Unlock()

	return n, err
}

func (s *recordedStream) Close() error {
	s.tracker.mu.Lock()
	s.tracker.closed = true
	s.tracker.mu.Unlock()

	return s.ReadCloser.Close()
}

// download takes a source apart and downloads it, the way a caller does.
func download(ctx context.Context, rawURL string, dst io.Writer, tracker ProgressTracker) (int64, error) {
	source, err := Parse(rawURL)
	if err != nil {
		return 0, err
	}

	return source.Download(ctx, dst, tracker)
}

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}

func TestParse_Name(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		want      string
		wantError string
	}{
		{
			name:   "archive",
			source: "https://files.gameap.ru/half-life/hlds.tar.xz",
			want:   "hlds.tar.xz",
		},
		{
			name:   "plain_http",
			source: "http://mirror.example.com/minecraft/server.jar",
			want:   "server.jar",
		},
		{
			name:   "query_is_not_part_of_the_name",
			source: "https://example.com/files/game.zip?token=abc&archive=false",
			want:   "game.zip",
		},
		{
			name:   "fragment_is_not_part_of_the_name",
			source: "https://example.com/files/game.zip#latest",
			want:   "game.zip",
		},
		{
			name:   "escaped_characters_are_decoded",
			source: "https://example.com/files/my%20game.zip",
			want:   "my game.zip",
		},
		{
			name:   "escaped_separator_does_not_reach_above_the_name",
			source: "https://example.com/files/..%2F..%2Fetc%2Fpasswd",
			want:   "passwd",
		},
		{
			name:   "port_and_credentials",
			source: "https://user:secret@example.com:8443/repo/game.7z",
			want:   "game.7z",
		},
		{
			name:      "no_path",
			source:    "https://example.com",
			wantError: `"https://example.com" names no file: unsupported source`,
		},
		{
			name:      "path_names_a_directory",
			source:    "https://example.com/files/",
			wantError: `"https://example.com/files/" names no file: unsupported source`,
		},
		{
			name:      "path_ends_above_itself",
			source:    "https://example.com/files/..",
			wantError: `names no file: unsupported source`,
		},
		{
			name:      "backslash_in_the_name",
			source:    `https://example.com/files/..%5Cgame.zip`,
			wantError: `names no file: unsupported source`,
		},
		{
			name:      "password_is_not_repeated_in_the_error",
			source:    "https://user:secret@example.com/",
			wantError: `"https://user:xxxxx@example.com/" names no file`,
		},
		{
			name:      "ftp",
			source:    "ftp://example.com/game.zip",
			wantError: `"ftp://example.com/game.zip": unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "local_file_url",
			source:    "file:///srv/gameap/repository/game.zip",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "s3_bucket",
			source:    "s3::https://s3.amazonaws.com/bucket/game.zip",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "git_repository",
			source:    "git::https://github.com/example/game.git",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "local_path",
			source:    "/srv/gameap/repository/game.zip",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "scheme_without_a_host",
			source:    "https:///game.zip",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "empty_source",
			source:    "",
			wantError: `unsupported source, a direct http or https link to a file is expected`,
		},
		{
			name:      "unparsable_source",
			source:    "https://exa mple.com/game.zip",
			wantError: "failed to parse the source: its host is not valid",
		},
		{
			name:      "unparsable_source_is_not_repeated_in_the_error",
			source:    "https://user:secret@example.com:port/game.zip",
			wantError: "failed to parse the source: it is not a valid URL",
		},
		{
			name:      "wrong_escape_in_the_password_is_not_repeated_in_the_error",
			source:    "https://user:secret%zz@example.com/game.zip",
			wantError: "failed to parse the source: it has a percent sign that is not followed by two hex digits, a literal one is written %25",
		},
		{
			name:      "password_with_a_slash_is_not_repeated_in_the_error",
			source:    "https://user:secret/tail@example.com/game.zip",
			wantError: "failed to parse the source: it is not a valid URL",
		},
		{
			name:      "wrong_escape_in_the_path_of_a_source_with_a_password",
			source:    "https://user:secret@example.com/game%zz.zip",
			wantError: "failed to parse the source: it has a percent sign that is not followed by two hex digits, a literal one is written %25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.source)

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.NotContains(t, err.Error(), "secret")
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Name())
		})
	}
}

func TestParse_UnsupportedSourceIsRecognizable(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"ftp://example.com/game.zip", "https://example.com/files/", "game.zip"} {
		_, err := Parse(source)

		require.ErrorIs(t, err, ErrUnsupportedSource, source)
	}
}

func TestDownload_StoresWhatTheSourceServes(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("game archive content ", 4096)

	var gotMethod, gotEncoding, gotPath, gotQuery string

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotEncoding = r.Header.Get("Accept-Encoding")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery

		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = io.WriteString(w, content)
	})

	var dst bytes.Buffer
	tracker := &recordingTracker{}
	source := server.URL + "/repo/game.tar.gz?token=abc"

	written, err := download(context.Background(), source, &dst, tracker)

	require.NoError(t, err)
	assert.Equal(t, int64(len(content)), written)
	assert.Equal(t, content, dst.String())

	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "identity", gotEncoding)
	assert.Equal(t, "/repo/game.tar.gz", gotPath)
	assert.Equal(t, "token=abc", gotQuery, "the query belongs to the source and is sent as it is")

	assert.Equal(t, 1, tracker.calls)
	assert.Equal(t, server.URL+"/repo/game.tar.gz", tracker.src, "the query does not go into the progress output")
	assert.Zero(t, tracker.current)
	assert.Equal(t, int64(len(content)), tracker.total)
	assert.Equal(t, int64(len(content)), tracker.bytesRead)
	assert.True(t, tracker.closed, "the stream handed out by the tracker has to be closed")
}

func TestDownload_WithoutATracker(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "tool")
	})

	var dst bytes.Buffer

	written, err := download(context.Background(), server.URL+"/tool", &dst, nil)

	require.NoError(t, err)
	assert.Equal(t, int64(4), written)
	assert.Equal(t, "tool", dst.String())
}

func TestDownload_UnknownLength(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming is not supported", http.StatusInternalServerError)

			return
		}

		for range 3 {
			_, _ = io.WriteString(w, "chunk ")
			flusher.Flush()
		}
	})

	var dst bytes.Buffer
	tracker := &recordingTracker{}

	written, err := download(context.Background(), server.URL+"/stream.bin", &dst, tracker)

	require.NoError(t, err)
	assert.Equal(t, int64(18), written)
	assert.Equal(t, "chunk chunk chunk ", dst.String())
	assert.Zero(t, tracker.total, "a length the server did not give is reported as unknown")
}

func TestDownload_FollowsRedirects(t *testing.T) {
	t.Parallel()

	mirror := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/storage/game.zip" {
			http.NotFound(w, r)

			return
		}

		_, _ = io.WriteString(w, "from the mirror")
	})

	origin := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mirror.URL+"/storage/game.zip", http.StatusFound)
	})

	var dst bytes.Buffer

	written, err := download(context.Background(), origin.URL+"/game.zip", &dst, nil)

	require.NoError(t, err)
	assert.Equal(t, int64(15), written)
	assert.Equal(t, "from the mirror", dst.String())
}

// A server may label what it sends as gzip encoded, whether asked to or not.
// The file is an archive under the name the link gave it and is kept as sent.
func TestDownload_KeepsAnEncodedBodyAsItIsSent(t *testing.T) {
	t.Parallel()

	var packed bytes.Buffer

	zw := gzip.NewWriter(&packed)
	_, err := zw.Write([]byte("tar stream"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/x-tar")
		_, _ = w.Write(packed.Bytes())
	})

	var dst bytes.Buffer

	written, err := download(context.Background(), server.URL+"/game.tar.gz", &dst, nil)

	require.NoError(t, err)
	assert.Equal(t, int64(packed.Len()), written)
	assert.Equal(t, packed.Bytes(), dst.Bytes())
}

// What a source is shown as, in the output of a task and in every error and
// progress line of this package, gives nobody access to the file: the password
// is left out, and so is the query, where a signed link keeps its token.
func TestSource_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "link_without_anything_to_hide",
			source: "https://example.com:8443/files/game.zip",
			want:   "https://example.com:8443/files/game.zip",
		},
		{
			name:   "password",
			source: "https://repo:secret@example.com/files/game.zip",
			want:   "https://repo:xxxxx@example.com/files/game.zip",
		},
		{
			name:   "user_without_a_password",
			source: "https://repo@example.com/files/game.zip",
			want:   "https://repo@example.com/files/game.zip",
		},
		{
			name:   "signed_link",
			source: "https://example.com/files/game.zip?X-Signature=secret&X-Expires=60",
			want:   "https://example.com/files/game.zip",
		},
		{
			name:   "query_that_is_a_token",
			source: "https://example.com/files/game.zip?secret",
			want:   "https://example.com/files/game.zip",
		},
		{
			name:   "empty_query",
			source: "https://example.com/files/game.zip?",
			want:   "https://example.com/files/game.zip",
		},
		{
			name:   "fragment",
			source: "https://example.com/files/game.zip#secret",
			want:   "https://example.com/files/game.zip",
		},
		{
			name: "parameters_of_the_daemon",
			source: "https://repo:secret@example.com/files/game.zip" +
				"?archive=zip&checksum=file:https://sums:secret@example.com/SHA256SUMS?key=secret",
			want: "https://repo:xxxxx@example.com/files/game.zip",
		},
		{
			name:   "escaped_path",
			source: "https://example.com/my%20files/game%231.zip",
			want:   "https://example.com/my%20files/game%231.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			source, err := Parse(tt.source)
			require.NoError(t, err)

			assert.Equal(t, tt.want, source.String())
			assert.Equal(t, tt.want, fmt.Sprint(source), "a source is printed the same way")
		})
	}
}

func TestDownload_SendsTheCredentialsOfTheSource(t *testing.T) {
	t.Parallel()

	var gotUser, gotPassword string

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPassword, _ = r.BasicAuth()

		_, _ = io.WriteString(w, "private")
	})

	source := strings.Replace(server.URL, "http://", "http://repo:secret@", 1) + "/private/game.zip"

	var dst bytes.Buffer
	tracker := &recordingTracker{}

	_, err := download(context.Background(), source, &dst, tracker)

	require.NoError(t, err)
	assert.Equal(t, "repo", gotUser)
	assert.Equal(t, "secret", gotPassword)
	assert.NotContains(t, tracker.src, "secret", "the password does not go into the progress output")
}

func TestDownload_ServerRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantError string
	}{
		{
			name:      "not_found",
			status:    http.StatusNotFound,
			wantError: `the server answered "404 Not Found"`,
		},
		{
			name:      "forbidden",
			status:    http.StatusForbidden,
			wantError: `the server answered "403 Forbidden"`,
		},
		{
			name:      "server_error",
			status:    http.StatusInternalServerError,
			wantError: `the server answered "500 Internal Server Error"`,
		},
		{
			name:      "no_content",
			status:    http.StatusNoContent,
			wantError: `the server answered "204 No Content"`,
		},
		{
			name:      "partial_content_that_was_not_asked_for",
			status:    http.StatusPartialContent,
			wantError: `the server answered "206 Partial Content"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)

				if tt.status != http.StatusNoContent {
					_, _ = io.WriteString(w, "<html>an error page that is no archive</html>")
				}
			})

			source := strings.Replace(server.URL, "http://", "http://repo:secret@", 1) + "/game.zip?token=secret"

			var dst bytes.Buffer
			tracker := &recordingTracker{}

			written, err := download(context.Background(), source, &dst, tracker)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Contains(t, err.Error(), `/game.zip"`)
			assert.NotContains(t, err.Error(), "secret", "neither the password nor the query is repeated")
			assert.Zero(t, written)
			assert.Zero(t, dst.Len(), "the error page is not stored as the file")
			assert.Zero(t, tracker.calls)
		})
	}
}

func TestDownload_ConnectionBreaksBeforeTheBodyIsComplete(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1024")
		_, _ = io.WriteString(w, "only the beginning")
	})

	var dst bytes.Buffer
	tracker := &recordingTracker{}

	written, err := download(context.Background(), server.URL+"/game.zip", &dst, tracker)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download")
	assert.Contains(t, err.Error(), "unexpected EOF")
	assert.Equal(t, int64(18), written)
	assert.True(t, tracker.closed)
}

func TestDownload_ServerIsNotThere(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()

	source := strings.Replace(address, "http://", "http://repo:secret@", 1) + "/game.zip?token=secret"

	var dst bytes.Buffer

	written, err := download(context.Background(), source, &dst, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to download "`+strings.Replace(address, "http://", "http://repo:xxxxx@", 1)+`/game.zip": `)
	assert.NotContains(t, err.Error(), "secret", "the client names a request it failed by its link, query and all")
	assert.NotContains(t, err.Error(), "token")
	assert.Zero(t, written)
	assert.Zero(t, dst.Len())
}

// A redirect leads to wherever the server says, which is often a signed link
// of a storage. A request that fails there is told of by the link of the
// source, and the one it was redirected to is not repeated.
func TestDownload_RedirectLeadsToAServerThatIsNotThere(t *testing.T) {
	t.Parallel()

	storage := httptest.NewServer(http.NotFoundHandler())
	signed := storage.URL + "/storage/game.zip?signature=secret"
	storage.Close()

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, signed, http.StatusFound)
	})

	var dst bytes.Buffer

	written, err := download(context.Background(), server.URL+"/game.zip", &dst, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to download "`+server.URL+`/game.zip": `)
	assert.NotContains(t, err.Error(), "secret")
	assert.NotContains(t, err.Error(), "signature")
	assert.Zero(t, written)
	assert.Zero(t, dst.Len())
}

// cancelingWriter cancels the context of the download once the first bytes
// arrive, the way an aborted installation does in the middle of an archive.
type cancelingWriter struct {
	written int
	cancel  context.CancelFunc
}

func (w *cancelingWriter) Write(p []byte) (int, error) {
	w.written += len(p)
	w.cancel()

	return len(p), nil
}

func TestDownload_CanceledWhileTheBodyArrives(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming is not supported", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Length", "1048576")
		_, _ = io.WriteString(w, "first bytes")
		flusher.Flush()

		select {
		case <-r.Context().Done():
		case <-released:
		}
	})
	t.Cleanup(func() { close(released) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	dst := &cancelingWriter{cancel: cancel}
	tracker := &recordingTracker{}

	written, err := download(ctx, server.URL+"/game.zip", dst, tracker)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int64(11), written)
	assert.Equal(t, 11, dst.written)
	assert.True(t, tracker.closed)
}

func TestDownload_CanceledBeforeItStarts(t *testing.T) {
	t.Parallel()

	requested := false

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requested = true

		_, _ = io.WriteString(w, "content")
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var dst bytes.Buffer

	written, err := download(ctx, server.URL+"/game.zip", &dst, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, written)
	assert.False(t, requested)
}

var errDiskFull = errors.New("no space left on device")

type fullDisk struct{}

func (fullDisk) Write([]byte) (int, error) {
	return 0, errDiskFull
}

func TestDownload_DestinationRefusesTheBytes(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "content")
	})

	tracker := &recordingTracker{}

	written, err := download(context.Background(), server.URL+"/game.zip", fullDisk{}, tracker)

	require.ErrorIs(t, err, errDiskFull)
	assert.Contains(t, err.Error(), "failed to download")
	assert.Zero(t, written)
	assert.True(t, tracker.closed)
}

func TestDownload_UnsupportedSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "ftp", source: "ftp://example.com/game.zip"},
		{name: "local_file_url", source: "file:///etc/passwd"},
		{name: "local_path", source: "/etc/passwd"},
		{name: "s3_bucket", source: "s3::https://s3.amazonaws.com/bucket/game.zip"},
		{name: "git_repository", source: "git::https://github.com/example/game.git"},
		{name: "empty_source", source: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dst bytes.Buffer
			tracker := &recordingTracker{}

			written, err := download(context.Background(), tt.source, &dst, tracker)

			require.ErrorIs(t, err, ErrUnsupportedSource)
			assert.Zero(t, written)
			assert.Zero(t, dst.Len())
			assert.Zero(t, tracker.calls)
		})
	}
}
