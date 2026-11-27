package fetch

import (
	"context"
	"crypto/md5"  //nolint:gosec // md5 is what many repositories publish for their files
	"crypto/sha1" //nolint:gosec // sha1 is what many repositories publish for their files
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/pkg/errors"
)

// maxChecksumFileSize is how much of a checksum file is read. One that lists
// every file of a large repository stays far below it.
const maxChecksumFileSize = 4 << 20

// checksumFilePrefix marks a checksum parameter that is the link to a checksum
// file and not the sum.
const checksumFilePrefix = "file"

type algorithm struct {
	name    string
	size    int
	newHash func() hash.Hash
}

// The sizes differ, which is what tells the type of a sum given without one.
var algorithms = []algorithm{
	{name: "md5", size: md5.Size, newHash: md5.New},    //nolint:gosec // see the import
	{name: "sha1", size: sha1.Size, newHash: sha1.New}, //nolint:gosec // see the import
	{name: "sha256", size: sha256.Size, newHash: sha256.New},
	{name: "sha512", size: sha512.Size, newHash: sha512.New},
}

type checksum struct {
	algorithm algorithm
	sum       []byte
}

// expectation is what a source says about the checksum of its file: the sum
// itself, or the link to a file that lists it. A source that says nothing has
// neither.
type expectation struct {
	sum  *checksum
	list *url.URL
}

// parseExpectation reads the checksum parameter of a source: "<hex>",
// "<type>:<hex>" or "file:<link>".
func parseExpectation(value string) (expectation, error) {
	if value == "" {
		return expectation{}, nil
	}

	kind, rest, typed := strings.Cut(value, ":")

	if !typed {
		sum, err := newChecksum("", value)

		return expectation{sum: sum}, err
	}

	if kind == checksumFilePrefix {
		list, err := url.Parse(rest)
		if err != nil {
			return expectation{}, parseError("the link to the checksum file", err)
		}

		return expectation{list: list}, requireHTTP(list)
	}

	if _, ok := algorithmByName(kind); !ok {
		return expectation{}, errors.Errorf(
			"unsupported checksum type %q: md5, sha1, sha256 or sha512 is expected, or file for the link to a checksum file",
			kind,
		)
	}

	sum, err := newChecksum(kind, rest)

	return expectation{sum: sum}, err
}

func algorithmByName(name string) (algorithm, bool) {
	for _, a := range algorithms {
		if a.name == strings.ToLower(name) {
			return a, true
		}
	}

	return algorithm{}, false
}

// newChecksum makes a checksum of its hex notation. With no name for the
// algorithm it is told by the length of the sum.
func newChecksum(algorithmName, value string) (*checksum, error) {
	sum, err := hex.DecodeString(value)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode the checksum, hex digits are expected")
	}

	if algorithmName != "" {
		a, ok := algorithmByName(algorithmName)

		switch {
		case !ok:
			return nil, errors.Errorf("unsupported checksum type %q", algorithmName)
		case a.size != len(sum):
			return nil, errors.Errorf("a %s checksum has %d hex digits, %d are given", a.name, a.size*2, len(value))
		}

		return &checksum{algorithm: a, sum: sum}, nil
	}

	for _, a := range algorithms {
		if a.size == len(sum) {
			return &checksum{algorithm: a, sum: sum}, nil
		}
	}

	return nil, errors.Errorf(
		"a checksum of %d hex digits is of no known type: md5 has 32, sha1 40, sha256 64 and sha512 128", len(value),
	)
}

// expectedChecksum returns what the file of the source has to hash to, and nil
// for a source that does not say. A checksum file is requested for it.
func (s *Source) expectedChecksum(ctx context.Context) (*checksum, error) {
	if s.expected.list == nil {
		return s.expected.sum, nil
	}

	list := s.expected.list

	resp, err := get(ctx, list)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to get the checksum file")
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumFileSize+1))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to download the checksum file %q", list.Redacted())
	}

	if len(content) > maxChecksumFileSize {
		return nil, errors.Errorf(
			"checksum file %q is larger than the %d bytes that are read of one", list.Redacted(), maxChecksumFileSize,
		)
	}

	sum, ok := findListed(content, listedNames(s.link, list))
	if !ok {
		return nil, errors.Errorf("checksum file %q has no checksum for %q", list.Redacted(), s.name)
	}

	return sum, nil
}

// listedNames returns the names a checksum file may list the file of a source
// under: its name, its path as seen from the directory of the checksum file
// when the file lies below it, and its whole path.
func listedNames(source, list *url.URL) []string {
	names := []string{path.Base(source.Path), source.Path}

	dir := strings.TrimSuffix(path.Dir(list.Path), "/") + "/"
	if rel, ok := strings.CutPrefix(source.Path, dir); ok {
		names = append(names, rel)
	}

	return names
}

// findListed looks a file up in a checksum file by the names it may be listed
// under. A line that gives a sum and no name is taken to be about the file,
// as the checksum file of a single download is written. The first line that
// fits is the answer, and a line that is not understood is passed over.
func findListed(content []byte, names []string) (*checksum, bool) {
	for line := range strings.SplitSeq(string(content), "\n") {
		sum, name, ok := parseListedLine(line)
		if !ok {
			continue
		}

		// "*" marks a file read in binary mode, "?" one read in portable mode.
		if len(name) > 1 && (name[0] == '*' || name[0] == '?') {
			name = name[1:]
		}

		name = strings.TrimPrefix(name, "./")

		if name == "" || slices.Contains(names, name) {
			return sum, true
		}
	}

	return nil, false
}

// parseListedLine reads one line of a checksum file in either of the two
// notations in use:
//
//	SHA256 (game.zip) = <hex>    BSD, also "sha256sum --tag"
//	<hex>  game.zip              GNU, "<hex> *game.zip" for a binary file
//
// The name comes back empty for a line that holds the sum alone.
func parseListedLine(line string) (*checksum, string, bool) {
	const bsdSeparator = ") = "

	line = strings.TrimSpace(line)

	if algorithmName, rest, ok := strings.Cut(line, " ("); ok {
		if end := strings.LastIndex(rest, bsdSeparator); end >= 0 {
			sum, err := newChecksum(algorithmName, strings.TrimSpace(rest[end+len(bsdSeparator):]))
			if err == nil {
				return sum, rest[:end], true
			}
		}
	}

	value, name := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		value, name = line[:i], strings.TrimLeft(line[i:], " \t")
	}

	sum, err := newChecksum("", value)
	if err != nil {
		return nil, "", false
	}

	return sum, name, true
}
