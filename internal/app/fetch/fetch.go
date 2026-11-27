// Package fetch downloads the files the daemon installs for the operator: game
// server archives and tools.
//
// A source is a direct HTTP or HTTPS link to one file. Nothing else is
// understood: no other scheme, no query parameter that changes what is done
// with the file, no instruction from the server about where else to look. What
// the link serves is written to the writer the caller hands over, and what
// becomes of it afterwards is the caller's decision alone.
package fetch

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
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

// ErrUnsupportedSource is returned for a source that is not an HTTP or HTTPS
// link to a file.
var ErrUnsupportedSource = errors.New("unsupported source, a direct http or https link to a file is expected")

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

// FileName returns what the file a source serves is called: the last element
// of the path the link names. Neither a redirect nor a response header changes
// it, so the name is known, and can be judged, before anything is requested.
func FileName(rawURL string) (string, error) {
	source, err := parseSource(rawURL)
	if err != nil {
		return "", err
	}

	name := path.Base(source.Path)

	if strings.HasSuffix(source.Path, "/") || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.WithMessagef(ErrUnsupportedSource, "%q names no file", source.Redacted())
	}

	return name, nil
}

// Download writes what the source serves to dst and returns the number of
// bytes written. Anything but a complete 200 response is an error, and dst is
// then left with whatever had arrived.
func Download(ctx context.Context, rawURL string, dst io.Writer, tracker ProgressTracker) (int64, error) {
	source, err := parseSource(rawURL)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.String(), nil)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to build a request for %q", source.Redacted())
	}

	// The file is stored as it is served. Left to itself the transport asks for
	// gzip and unpacks the answer, and a server that labels a .tar.gz as gzip
	// encoded would have its archive stored under a name it no longer matches.
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := client.Do(req)
	if err != nil {
		return 0, errors.Wrap(err, "failed to download")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.Errorf("failed to download %q: the server answered %q", source.Redacted(), resp.Status)
	}

	body := resp.Body
	if tracker != nil {
		body = tracker.TrackProgress(source.Redacted(), 0, max(resp.ContentLength, 0), resp.Body)
	}

	written, copyErr := io.Copy(dst, body)
	closeErr := body.Close()

	if copyErr != nil {
		return written, errors.Wrapf(copyErr, "failed to download %q", source.Redacted())
	}
	if closeErr != nil {
		return written, errors.Wrapf(closeErr, "failed to finish the download of %q", source.Redacted())
	}

	return written, nil
}

func parseSource(rawURL string) (*url.URL, error) {
	source, err := url.Parse(rawURL)
	if err != nil {
		return nil, parseError(err)
	}

	if (source.Scheme != "http" && source.Scheme != "https") || source.Host == "" {
		return nil, errors.WithMessagef(ErrUnsupportedSource, "%q", source.Redacted())
	}

	return source, nil
}

// parseError tells why a source could not be parsed without repeating any of
// it. The source may carry credentials, and it cannot be redacted the way a
// parsed one is: the error of url.Parse quotes it as a whole, and the reason
// inside that error quotes the part that is wrong, which a password with a
// slash or a percent sign in it turns out to be.
func parseError(err error) error {
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

	return errors.New("failed to parse the source: " + reason)
}
