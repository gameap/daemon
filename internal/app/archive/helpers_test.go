package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeTree creates files (and their parent directories) under dir. Map keys
// are slash-separated relative paths.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

// readTree reads every regular file under dir into a slash-separated
// relative-path map.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		files[filepath.ToSlash(rel)] = string(content)

		return nil
	})
	require.NoError(t, err)

	return files
}

// copyFixture copies a repo fixture from test/files into the work directory.
func copyFixture(t *testing.T, workDir, name string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "files", name))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, name), data, 0o644))
}

type progressRecord struct {
	files   int64
	bytes   int64
	entries []string
}

func (p *progressRecord) fn() ProgressFunc {
	return func(filesProcessed, bytesProcessed int64, currentEntry string) {
		p.files = filesProcessed
		p.bytes = bytesProcessed
		p.entries = append(p.entries, currentEntry)
	}
}

func canceledContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}
