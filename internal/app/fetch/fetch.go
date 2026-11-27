// Package fetch downloads the files the daemon installs for the operator: game
// server archives and tools.
//
// A source is a direct HTTP or HTTPS link to one file. Nothing else is
// understood: no other scheme, no instruction from the server about where else
// to look. What the link serves is written to the writer the caller hands over,
// and what becomes of it afterwards is the caller's decision alone.
//
// Two parameters of the query are addressed to the daemon and not to the
// server. They are taken out of the link before it is requested:
//
//   - checksum is what the file has to hash to, and a file that does not is a
//     download that failed. It is the sum itself, "<hex>" or "<type>:<hex>"
//     with md5, sha1, sha256 or sha512 for the type, or "file:<link>" for a
//     file that lists it the way sha256sum and its kin write one;
//   - archive says whether the file is to be unpacked, and as what. The
//     package does not unpack and only hands the value over: see
//     Source.Archive.
package fetch

import (
	"bytes"
	"context"
	"encoding/hex"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	dialTimeout           = 30 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	responseHeaderTimeout = 60 * time.Second
	idleConnTimeout       = 90 * time.Second
)

const (
	checksumParam = "checksum"
	archiveParam  = "archive"
)

var daemonParams = []string{checksumParam, archiveParam}

// ErrUnsupportedSource is returned for a source that is not an HTTP or HTTPS
// link to a file.
var ErrUnsupportedSource = errors.New("unsupported source, a direct http or https link to a file is expected")

// ErrChecksumMismatch is returned for a file that was downloaded and does not
// hash to the checksum its source gives.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// A download has no deadline of its own: a game server archive takes as long
// as it takes, and the caller's context ends it. The timeouts only cover the
// steps that have nothing to wait for, so that a mirror that accepts the
// connection and never answers is given up on.
var client = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   dialTimeout,
			KeepAlive: dialTimeout,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       idleConnTimeout,
		ExpectContinueTimeout: time.Second,
	},
}

// Source is a link to a file, taken apart: the link that is requested and what
// its parameters tell the daemon about the file.
type Source struct {
	link     *url.URL
	name     string
	archive  string
	expected expectation
}

// Parse takes a source apart. Whatever is wrong with it that shows without a
// request is found here, a checksum that cannot be one included: nothing is
// downloaded to be turned away afterwards.
func Parse(rawURL string) (*Source, error) {
	link, err := url.Parse(rawURL)
	if err != nil {
		return nil, parseError("the source", err)
	}

	// Before anything is said about the link: the parameters are not repeated
	// in an error, the link to a checksum file may carry a password.
	params, paramsErr := takeParams(link)

	if err = requireHTTP(link); err != nil {
		return nil, err
	}

	if paramsErr != nil {
		return nil, paramsErr
	}

	name := path.Base(link.Path)

	if strings.HasSuffix(link.Path, "/") || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return nil, errors.WithMessagef(ErrUnsupportedSource, "%q names no file", link.Redacted())
	}

	expected, err := parseExpectation(params[checksumParam])
	if err != nil {
		return nil, errors.WithMessage(err, "invalid checksum parameter of the source")
	}

	return &Source{
		link:     link,
		name:     name,
		archive:  params[archiveParam],
		expected: expected,
	}, nil
}

// Name returns what the file the source serves is called: the last element of
// the path the link names. Neither a redirect nor a response header changes
// it, so the name is known, and can be judged, before anything is requested.
func (s *Source) Name() string {
	return s.name
}

// Archive returns the archive parameter of the source as it is written, and an
// empty string for a source without one. What it stands for is the caller's to
// decide, as is whether the file is unpacked at all.
func (s *Source) Archive() string {
	return s.archive
}

// Download writes what the source serves to dst and returns the number of
// bytes written. Anything but a complete 200 response is an error, and so is a
// file that does not hash to the checksum of the source. dst is then left with
// whatever had arrived: it is the caller's to throw away.
func (s *Source) Download(ctx context.Context, dst io.Writer, tracker ProgressTracker) (int64, error) {
	// Before the file: a checksum file that cannot be had, or that does not
	// list the file, fails the download before an archive is fetched for it.
	want, err := s.expectedChecksum(ctx)
	if err != nil {
		return 0, err
	}

	resp, err := get(ctx, s.link)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body := resp.Body
	if tracker != nil {
		body = tracker.TrackProgress(s.link.Redacted(), 0, max(resp.ContentLength, 0), resp.Body)
	}

	var hasher hash.Hash

	out := dst
	if want != nil {
		hasher = want.algorithm.newHash()
		out = io.MultiWriter(dst, hasher)
	}

	written, copyErr := io.Copy(out, body)
	closeErr := body.Close()

	if copyErr != nil {
		return written, errors.Wrapf(copyErr, "failed to download %q", s.link.Redacted())
	}
	if closeErr != nil {
		return written, errors.Wrapf(closeErr, "failed to finish the download of %q", s.link.Redacted())
	}

	if want == nil {
		return written, nil
	}

	if got := hasher.Sum(nil); !bytes.Equal(got, want.sum) {
		return written, errors.WithMessagef(
			ErrChecksumMismatch,
			"failed to verify %q: %s %s is expected and the file has %s",
			s.link.Redacted(), want.algorithm.name, hex.EncodeToString(want.sum), hex.EncodeToString(got),
		)
	}

	return written, nil
}

// get requests link and returns the response once it is a 200 one. The body is
// the caller's to close.
func get(ctx context.Context, link *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build a request for %q", link.Redacted())
	}

	// The file is stored as it is served. Left to itself the transport asks for
	// gzip and unpacks the answer, and a server that labels a .tar.gz as gzip
	// encoded would have its archive stored under a name it no longer matches.
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download")
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, errors.Errorf("failed to download %q: the server answered %q", link.Redacted(), resp.Status)
	}

	return resp, nil
}

// requireHTTP turns away a link the daemon does not request.
func requireHTTP(link *url.URL) error {
	if (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" {
		return errors.WithMessagef(ErrUnsupportedSource, "%q", link.Redacted())
	}

	return nil
}

// takeParams takes the parameters addressed to the daemon out of the query of
// link and returns them. The rest of the query stays as it is written: it is
// the server's, and a signed link does not survive being encoded anew.
//
// The parameters are taken out of the link also when they are returned with an
// error.
func takeParams(link *url.URL) (map[string]string, error) {
	var firstErr error

	params := make(map[string]string, len(daemonParams))
	rest := make([]string, 0, strings.Count(link.RawQuery, "&")+1)

	for part := range strings.SplitSeq(link.RawQuery, "&") {
		key, value, _ := strings.Cut(part, "=")

		name, err := url.QueryUnescape(key)
		if err != nil || !slices.Contains(daemonParams, name) {
			rest = append(rest, part)

			continue
		}

		if _, ok := params[name]; ok && firstErr == nil {
			firstErr = errors.Errorf("the source has more than one %s parameter", name)
		}

		params[name], err = url.QueryUnescape(value)
		if err != nil && firstErr == nil {
			firstErr = errors.Errorf(
				"the %s parameter of the source has a percent sign that is not followed by two hex digits", name,
			)
		}
	}

	link.RawQuery = strings.Join(rest, "&")

	return params, firstErr
}

// parseError tells why a link could not be parsed without repeating any of it.
// The link may carry credentials, and it cannot be redacted the way a parsed
// one is: the error of url.Parse quotes it as a whole, and the reason inside
// that error quotes the part that is wrong, which a password with a slash or a
// percent sign in it turns out to be.
func parseError(what string, err error) error {
	var (
		escapeErr url.EscapeError
		hostErr   url.InvalidHostError
	)

	reason := "it is not a valid URL"

	switch {
	case errors.As(err, &escapeErr):
		reason = "it has a percent sign that is not followed by two hex digits, a literal one is written %25"
	case errors.As(err, &hostErr):
		reason = "its host is not valid"
	}

	return errors.New("failed to parse " + what + ": " + reason)
}
