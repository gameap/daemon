package gameservercommands

import (
	"net/url"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// buildRemoteRepositoryCandidates expands a remote repository URL into a list
// of download candidates: replacement targets configured for the URL host in
// priority order, followed by the original URL as the last resort.
// Without matching replacements the original URL is returned as the only candidate.
func buildRemoteRepositoryCandidates(
	remoteRepository string,
	replacements config.RepositoryReplacements,
) []string {
	originalURL, err := url.Parse(remoteRepository)
	if err != nil || originalURL.Host == "" {
		return []string{remoteRepository}
	}

	targets := replacements.TargetsForURL(originalURL)
	if len(targets) == 0 {
		return []string{remoteRepository}
	}

	candidates := make([]string, 0, len(targets)+1)
	seen := make(map[string]struct{}, len(targets)+1)
	appendCandidate := func(candidate string) {
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, target := range targets.Sorted() {
		replaced, err := replaceRepositoryURL(originalURL, target)
		if err != nil {
			log.Warning(errors.WithMessagef(
				err,
				"[game_server_commands.repositoryReplacer] skipped replacement for %q",
				remoteRepository,
			))

			continue
		}

		appendCandidate(replaced)
	}

	appendCandidate(remoteRepository)

	return candidates
}

// replaceRepositoryURL applies a replacement target to the URL: the host is
// replaced, the scheme and the path prefix are overridden only when present
// in the target. The rest of the URL (path, query, fragment) is preserved.
func replaceRepositoryURL(originalURL *url.URL, target config.RepositoryReplacementTarget) (string, error) {
	replaceURL, err := target.URL()
	if err != nil {
		return "", err
	}

	result := *originalURL
	result.Host = replaceURL.Host

	if replaceURL.Scheme != "" {
		result.Scheme = replaceURL.Scheme
	}

	if replaceURL.User != nil {
		result.User = replaceURL.User
	}

	if path := strings.TrimSuffix(replaceURL.Path, "/"); path != "" {
		result.Path = path + originalURL.Path
		result.RawPath = ""
	}

	return result.String(), nil
}
