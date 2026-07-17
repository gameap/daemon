package gameservercommands

import (
	"testing"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/stretchr/testify/assert"
)

func TestBuildRemoteRepositoryCandidates(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		replacements config.RepositoryReplacements
		expected     []string
	}{
		{
			name:     "no replacements configured",
			url:      "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			expected: []string{"http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz"},
		},
		{
			name: "no replacements for the host",
			url:  "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.example.com": {{Replace: "cdn.example.com"}},
			},
			expected: []string{"http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz"},
		},
		{
			name: "host replaced, original last",
			url:  "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com"}},
			},
			expected: []string{
				"http://cdn.gameap.com/cstrike-1.6/hlcs_base.tar.xz",
				"http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			},
		},
		{
			name: "scheme overridden by replacement",
			url:  "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "https://cdn.gameap.com"}},
			},
			expected: []string{
				"https://cdn.gameap.com/cstrike-1.6/hlcs_base.tar.xz",
				"http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			},
		},
		{
			name: "path prefix prepended",
			url:  "http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.example.com/gameap-mirror/"}},
			},
			expected: []string{
				"http://cdn.example.com/gameap-mirror/cstrike-1.6/hlcs_base.tar.xz",
				"http://files.gameap.ru/cstrike-1.6/hlcs_base.tar.xz",
			},
		},
		{
			name: "priority defines the order",
			url:  "http://files.gameap.ru/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "cdn.gameap.ru", Priority: 9},
					{Replace: "cdn.gameap.com", Priority: 10},
				},
			},
			expected: []string{
				"http://cdn.gameap.com/hlcs_base.tar.xz",
				"http://cdn.gameap.ru/hlcs_base.tar.xz",
				"http://files.gameap.ru/hlcs_base.tar.xz",
			},
		},
		{
			name: "equal priorities keep the config order",
			url:  "http://files.gameap.ru/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "cdn1.gameap.com"},
					{Replace: "cdn2.gameap.com"},
					{Replace: "cdn3.gameap.com"},
				},
			},
			expected: []string{
				"http://cdn1.gameap.com/hlcs_base.tar.xz",
				"http://cdn2.gameap.com/hlcs_base.tar.xz",
				"http://cdn3.gameap.com/hlcs_base.tar.xz",
				"http://files.gameap.ru/hlcs_base.tar.xz",
			},
		},
		{
			name: "replacement with port for URL with port",
			url:  "http://files.gameap.ru:8080/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com:9090"}},
			},
			expected: []string{
				"http://cdn.gameap.com:9090/hlcs_base.tar.xz",
				"http://files.gameap.ru:8080/hlcs_base.tar.xz",
			},
		},
		{
			name: "query preserved",
			url:  "http://files.gameap.ru/hlcs_base.tar.xz?token=abc",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com"}},
			},
			expected: []string{
				"http://cdn.gameap.com/hlcs_base.tar.xz?token=abc",
				"http://files.gameap.ru/hlcs_base.tar.xz?token=abc",
			},
		},
		{
			name: "invalid replacement skipped",
			url:  "http://files.gameap.ru/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "  ", Priority: 10},
					{Replace: "cdn.gameap.com", Priority: 9},
				},
			},
			expected: []string{
				"http://cdn.gameap.com/hlcs_base.tar.xz",
				"http://files.gameap.ru/hlcs_base.tar.xz",
			},
		},
		{
			name: "replacement equal to the original deduplicated",
			url:  "http://files.gameap.ru/hlcs_base.tar.xz",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "files.gameap.ru", Priority: 10},
					{Replace: "cdn.gameap.com", Priority: 9},
				},
			},
			expected: []string{
				"http://files.gameap.ru/hlcs_base.tar.xz",
				"http://cdn.gameap.com/hlcs_base.tar.xz",
			},
		},
		{
			name: "unparsable URL returned as is",
			url:  "://invalid",
			replacements: config.RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com"}},
			},
			expected: []string{"://invalid"},
		},
		{
			name: "URL without host returned as is",
			url:  "invalid-value",
			replacements: config.RepositoryReplacements{
				"invalid-value": {{Replace: "cdn.gameap.com"}},
			},
			expected: []string{"invalid-value"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidates := buildRemoteRepositoryCandidates(test.url, test.replacements)

			assert.Equal(t, test.expected, candidates)
		})
	}
}
