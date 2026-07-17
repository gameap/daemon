package config

import (
	"net/url"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryReplacements_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected RepositoryReplacements
	}{
		{
			name: "single string",
			yaml: `
remote_repository_replacements:
  files.gameap.ru: cdn.gameap.com
`,
			expected: RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com"}},
			},
		},
		{
			name: "list of strings",
			yaml: `
remote_repository_replacements:
  files.gameap.ru:
    - cdn.gameap.com
    - cdn.gameap.ru
`,
			expected: RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "cdn.gameap.com"},
					{Replace: "cdn.gameap.ru"},
				},
			},
		},
		{
			name: "list of objects with priority",
			yaml: `
remote_repository_replacements:
  files.gameap.ru:
    - replace: cdn.gameap.com
      priority: 10
    - replace: cdn.gameap.ru
      priority: 9
`,
			expected: RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "cdn.gameap.com", Priority: 10},
					{Replace: "cdn.gameap.ru", Priority: 9},
				},
			},
		},
		{
			name: "mixed list of strings and objects",
			yaml: `
remote_repository_replacements:
  files.gameap.ru:
    - cdn.gameap.com
    - replace: cdn.gameap.ru
      priority: 5
`,
			expected: RepositoryReplacements{
				"files.gameap.ru": {
					{Replace: "cdn.gameap.com"},
					{Replace: "cdn.gameap.ru", Priority: 5},
				},
			},
		},
		{
			name: "multiple hosts",
			yaml: `
remote_repository_replacements:
  files.gameap.ru: cdn.gameap.com
  files.example.com:
    - cdn.example.com
`,
			expected: RepositoryReplacements{
				"files.gameap.ru":   {{Replace: "cdn.gameap.com"}},
				"files.example.com": {{Replace: "cdn.example.com"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &Config{}

			err := yaml.Unmarshal([]byte(test.yaml), cfg)

			require.NoError(t, err)
			assert.Equal(t, test.expected, cfg.RemoteRepositoryReplacements)
		})
	}
}

func TestRepositoryReplacements_UnmarshalYAML_InvalidValue_ExpectError(t *testing.T) {
	cfg := &Config{}

	err := yaml.Unmarshal([]byte(`
remote_repository_replacements:
  files.gameap.ru:
    replace: cdn.gameap.com
`), cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string or a list")
}

func TestRepositoryReplacements_Validate(t *testing.T) {
	tests := []struct {
		name          string
		replacements  RepositoryReplacements
		expectedError error
	}{
		{
			"empty host key",
			RepositoryReplacements{"  ": {{Replace: "cdn.gameap.com"}}},
			ErrEmptyReplacementKey,
		},
		{
			"duplicate host key",
			RepositoryReplacements{
				"files.gameap.ru": {{Replace: "cdn.gameap.com"}},
				"Files.GameAP.RU": {{Replace: "cdn.gameap.ru"}},
			},
			ErrDuplicateReplacementKey,
		},
		{
			"no targets",
			RepositoryReplacements{"files.gameap.ru": {}},
			ErrNoReplacementTargets,
		},
		{
			"empty replacement target",
			RepositoryReplacements{"files.gameap.ru": {{Replace: "  "}}},
			ErrEmptyReplacementTarget,
		},
		{
			"replacement without host",
			RepositoryReplacements{"files.gameap.ru": {{Replace: "https://"}}},
			ErrEmptyReplacementHost,
		},
		{
			"replacement with query",
			RepositoryReplacements{"files.gameap.ru": {{Replace: "cdn.gameap.com/mirror?token=x"}}},
			ErrReplacementHasQueryOrFragment,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := givenValidConfig(t)
			cfg.RemoteRepositoryReplacements = test.replacements

			err := cfg.Init()

			assert.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestRepositoryReplacements_Validate_ValidConfig(t *testing.T) {
	cfg := givenValidConfig(t)
	cfg.RemoteRepositoryReplacements = RepositoryReplacements{
		"files.gameap.ru": {
			{Replace: "cdn.gameap.com", Priority: 10},
			{Replace: "https://mirror.gameap.ru/files", Priority: 9},
			{Replace: "cdn.gameap.ru:8080"},
		},
	}

	err := cfg.Init()

	assert.NoError(t, err)
}

func TestRepositoryReplacements_TargetsForURL(t *testing.T) {
	replacements := RepositoryReplacements{
		"files.gameap.ru":      {{Replace: "cdn.gameap.com"}},
		"files.gameap.ru:8080": {{Replace: "cdn-alt.gameap.com"}},
		"Files.Example.COM":    {{Replace: "cdn.example.com"}},
	}

	tests := []struct {
		name     string
		url      string
		expected RepositoryReplacementTargets
	}{
		{
			"key without port matches URL without port",
			"http://files.gameap.ru/game.tar.xz",
			RepositoryReplacementTargets{{Replace: "cdn.gameap.com"}},
		},
		{
			"key with port wins for URL with matching port",
			"http://files.gameap.ru:8080/game.tar.xz",
			RepositoryReplacementTargets{{Replace: "cdn-alt.gameap.com"}},
		},
		{
			"key without port matches URL with any other port",
			"http://files.gameap.ru:9090/game.tar.xz",
			RepositoryReplacementTargets{{Replace: "cdn.gameap.com"}},
		},
		{
			"matching is case-insensitive",
			"http://FILES.example.com/game.tar.xz",
			RepositoryReplacementTargets{{Replace: "cdn.example.com"}},
		},
		{
			"no match",
			"http://other.gameap.ru/game.tar.xz",
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u, err := url.Parse(test.url)
			require.NoError(t, err)

			assert.Equal(t, test.expected, replacements.TargetsForURL(u))
		})
	}
}

func TestRepositoryReplacementTargets_Sorted(t *testing.T) {
	targets := RepositoryReplacementTargets{
		{Replace: "third", Priority: 5},
		{Replace: "first", Priority: 10},
		{Replace: "fourth", Priority: 5},
		{Replace: "second", Priority: 7},
	}

	sorted := targets.Sorted()

	assert.Equal(t, RepositoryReplacementTargets{
		{Replace: "first", Priority: 10},
		{Replace: "second", Priority: 7},
		{Replace: "third", Priority: 5},
		{Replace: "fourth", Priority: 5},
	}, sorted)
	assert.Equal(t, RepositoryReplacementTargets{
		{Replace: "third", Priority: 5},
		{Replace: "first", Priority: 10},
		{Replace: "fourth", Priority: 5},
		{Replace: "second", Priority: 7},
	}, targets)
}

func TestRepositoryReplacementTarget_URL(t *testing.T) {
	tests := []struct {
		name           string
		replace        string
		expectedScheme string
		expectedHost   string
		expectedPath   string
		expectedError  error
	}{
		{name: "host only", replace: "cdn.gameap.com", expectedHost: "cdn.gameap.com"},
		{name: "host with port", replace: "cdn.gameap.com:8080", expectedHost: "cdn.gameap.com:8080"},
		{
			name:           "host with scheme",
			replace:        "https://cdn.gameap.com",
			expectedScheme: "https",
			expectedHost:   "cdn.gameap.com",
		},
		{
			name:         "host with path prefix",
			replace:      "cdn.gameap.com/mirror",
			expectedHost: "cdn.gameap.com",
			expectedPath: "/mirror",
		},
		{
			name:           "scheme, host, port and path prefix",
			replace:        "https://cdn.gameap.com:8080/mirror/files",
			expectedScheme: "https",
			expectedHost:   "cdn.gameap.com:8080",
			expectedPath:   "/mirror/files",
		},
		{name: "surrounding spaces are trimmed", replace: "  cdn.gameap.com  ", expectedHost: "cdn.gameap.com"},
		{name: "empty", replace: "", expectedError: ErrEmptyReplacementTarget},
		{name: "blank", replace: "   ", expectedError: ErrEmptyReplacementTarget},
		{name: "scheme without host", replace: "https://", expectedError: ErrEmptyReplacementHost},
		{name: "with query", replace: "cdn.gameap.com?a=b", expectedError: ErrReplacementHasQueryOrFragment},
		{name: "with fragment", replace: "cdn.gameap.com/mirror#frag", expectedError: ErrReplacementHasQueryOrFragment},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u, err := RepositoryReplacementTarget{Replace: test.replace}.URL()

			if test.expectedError != nil {
				assert.ErrorIs(t, err, test.expectedError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.expectedScheme, u.Scheme)
			assert.Equal(t, test.expectedHost, u.Host)
			assert.Equal(t, test.expectedPath, u.Path)
		})
	}
}
