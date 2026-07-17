package config

import (
	"net/url"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/pkg/errors"
)

// RepositoryReplacements maps a remote repository host (optionally with a port)
// to replacement targets tried in priority order before the original URL.
type RepositoryReplacements map[string]RepositoryReplacementTargets

type RepositoryReplacementTargets []RepositoryReplacementTarget

type RepositoryReplacementTarget struct {
	Replace  string `yaml:"replace"`
	Priority int    `yaml:"priority"`
}

// UnmarshalYAML accepts either a single scalar string or a list of
// strings/objects:
//
//	files.gameap.ru: cdn.gameap.com
//	files.gameap.ru:
//	  - cdn1.gameap.com
//	  - replace: cdn2.gameap.com
//	    priority: 10
func (t *RepositoryReplacementTargets) UnmarshalYAML(data []byte) error {
	var single string
	if err := yaml.Unmarshal(data, &single); err == nil {
		*t = RepositoryReplacementTargets{{Replace: single}}
		return nil
	}

	var targets []RepositoryReplacementTarget
	if err := yaml.Unmarshal(data, &targets); err != nil {
		return errors.Wrapf(
			err,
			"replacement must be a string or a list of strings/objects with 'replace' and 'priority', got %q",
			strings.TrimSpace(string(data)),
		)
	}

	*t = targets

	return nil
}

// UnmarshalYAML accepts either a scalar string (priority 0) or an object
// with 'replace' and 'priority' fields.
func (t *RepositoryReplacementTarget) UnmarshalYAML(data []byte) error {
	var single string
	if err := yaml.Unmarshal(data, &single); err == nil {
		*t = RepositoryReplacementTarget{Replace: single}
		return nil
	}

	type plain RepositoryReplacementTarget
	var target plain
	if err := yaml.Unmarshal(data, &target); err != nil {
		return errors.Wrapf(
			err,
			"replacement target must be a string or an object with 'replace' and 'priority', got %q",
			strings.TrimSpace(string(data)),
		)
	}

	*t = RepositoryReplacementTarget(target)

	return nil
}

// TargetsForURL returns the replacement targets configured for the URL host.
// A key with a port matches the URL "host:port" exactly, a key without a port
// matches the URL hostname regardless of the port; the more specific key wins.
// Matching is case-insensitive. Nil is returned when nothing matches.
func (r RepositoryReplacements) TargetsForURL(u *url.URL) RepositoryReplacementTargets {
	if len(r) == 0 || u == nil {
		return nil
	}

	host := strings.ToLower(u.Host)
	hostname := strings.ToLower(u.Hostname())

	var hostnameMatch RepositoryReplacementTargets

	for key, targets := range r {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case host:
			return targets
		case hostname:
			hostnameMatch = targets
		}
	}

	return hostnameMatch
}

// Sorted returns a copy ordered by priority, highest first.
// Targets with equal priority keep their configuration order.
func (t RepositoryReplacementTargets) Sorted() RepositoryReplacementTargets {
	sorted := slices.Clone(t)
	slices.SortStableFunc(sorted, func(a, b RepositoryReplacementTarget) int {
		return b.Priority - a.Priority
	})

	return sorted
}

// URL parses the replacement value as "[scheme://]host[:port][/path-prefix]".
// A value without a scheme keeps the scheme of the URL being replaced.
func (t RepositoryReplacementTarget) URL() (*url.URL, error) {
	value := strings.TrimSpace(t.Replace)
	if value == "" {
		return nil, ErrEmptyReplacementTarget
	}

	raw := value
	if !strings.Contains(raw, "://") {
		raw = "//" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid replacement %q", value)
	}

	if u.Host == "" {
		return nil, errors.WithMessagef(ErrEmptyReplacementHost, "invalid replacement %q", value)
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.WithMessagef(ErrReplacementHasQueryOrFragment, "invalid replacement %q", value)
	}

	return u, nil
}

func (r RepositoryReplacements) validate() error {
	normalizedKeys := make(map[string]string, len(r))

	for key, targets := range r {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			return errors.WithMessage(ErrEmptyReplacementKey, "remote_repository_replacements")
		}

		if duplicateOf, ok := normalizedKeys[normalizedKey]; ok {
			return errors.WithMessagef(
				ErrDuplicateReplacementKey,
				"remote_repository_replacements: %q and %q",
				duplicateOf, key,
			)
		}
		normalizedKeys[normalizedKey] = key

		if len(targets) == 0 {
			return errors.WithMessagef(ErrNoReplacementTargets, "remote_repository_replacements[%s]", key)
		}

		for _, target := range targets {
			if _, err := target.URL(); err != nil {
				return errors.WithMessagef(err, "remote_repository_replacements[%s]", key)
			}
		}
	}

	return nil
}
