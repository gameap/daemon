package archive

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/gameap/gameap/pkg/proto"
)

// treeDestination opens a fresh directory as the root an archive is unpacked
// through and returns it with its path.
func treeDestination(t *testing.T) (*os.Root, string) {
	t.Helper()

	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })

	return root, dir
}

func openArchive(t *testing.T, path string) *os.File {
	t.Helper()

	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	return f
}

func fixturePath(elem ...string) string {
	return filepath.Join(append([]string{"..", "..", "..", "test", "files"}, elem...)...)
}

func TestIsArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want bool
	}{
		{name: "tar_gz", file: "hlds.tar.gz", want: true},
		{name: "tar_xz", file: "hlcs_base.tar.xz", want: true},
		{name: "tgz", file: "game.tgz", want: true},
		{name: "zip", file: "game.zip", want: true},
		{name: "upper_case_extension", file: "GAME.ZIP", want: true},
		{name: "seven_zip", file: "game.7z", want: true},
		{name: "rar", file: "game.rar", want: true},
		{name: "single_compressed_file", file: "server.jar.gz", want: true},
		{name: "jar_is_kept_as_it_is", file: "server.jar", want: false},
		{name: "pk3_is_kept_as_it_is", file: "pak0.pk3", want: false},
		{name: "executable", file: "samp03svr", want: false},
		{name: "archive_extension_in_a_directory_name", file: "mods.zip/readme.txt", want: false},
		{name: "empty_name", file: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, IsArchiveName(tt.file))
		})
	}
}

func TestExtractTree_Formats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		archive    string
		wantFormat pb.ArchiveFormat
		wantFiles  map[string]string
	}{
		{
			name:       "tar_gz",
			archive:    fixturePath("local_repository", "game.tar.gz"),
			wantFormat: pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_GZ,
			wantFiles:  map[string]string{"game_file_from_tar_gz": "game_file_from_tar_gz\n"},
		},
		{
			name:       "tar_xz",
			archive:    fixturePath("local_repository", "game.tar.xz"),
			wantFormat: pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ,
			wantFiles:  map[string]string{"game_file_from_tar_xz": "game_file_from_tar_xz\n"},
		},
		{
			name:       "zip",
			archive:    fixturePath("local_repository", "game.zip"),
			wantFormat: pb.ArchiveFormat_ARCHIVE_FORMAT_ZIP,
			wantFiles:  map[string]string{"game_file_from_zip": "game_file_from_zip\n"},
		},
		{
			name:       "seven_zip",
			archive:    fixturePath("local_repository", "game.7z"),
			wantFormat: pb.ArchiveFormat_ARCHIVE_FORMAT_7Z,
			wantFiles:  map[string]string{"game_file_from_7z": "game_file_from_7z\n"},
		},
		{
			name:       "rar",
			archive:    fixturePath("test.rar"),
			wantFormat: pb.ArchiveFormat_ARCHIVE_FORMAT_RAR,
			wantFiles: map[string]string{
				"hello.txt":         "Hello, RAR!\n",
				"subdir/nested.txt": "nested rar content\n",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			progress := &progressRecord{}

			res, err := ExtractTree(
				context.Background(),
				openArchive(t, tt.archive),
				filepath.Base(tt.archive),
				root,
				TreeOptions{Progress: progress.fn()},
			)

			require.NoError(t, err)
			assert.Equal(t, tt.wantFormat, res.Format)
			assert.Equal(t, tt.wantFiles, readTree(t, dir))
			assert.Empty(t, res.SkippedLinks)
			assert.Positive(t, res.ArchiveSize)
			assert.Equal(t, res.FilesProcessed, progress.files)
			assert.GreaterOrEqual(t, res.FilesProcessed, int64(len(tt.wantFiles)))
		})
	}
}

// A download is stored under a name of the daemon's own choosing, so the
// content has to be enough to tell what the archive is.
func TestExtractTree_FormatIsReadOffTheContent(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	res, err := ExtractTree(
		context.Background(),
		openArchive(t, fixturePath("local_repository", "game.tar.xz")),
		"gameap-install-12345",
		root,
		TreeOptions{},
	)

	require.NoError(t, err)
	assert.Equal(t, pb.ArchiveFormat_ARCHIVE_FORMAT_TAR_XZ, res.Format)
	assert.Equal(t, map[string]string{"game_file_from_tar_xz": "game_file_from_tar_xz\n"}, readTree(t, dir))
}

func TestExtractTree_ArchiveIsReadFromItsStart(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	archive := openArchive(t, fixturePath("local_repository", "game.tar.gz"))
	_, err := archive.Seek(0, 2)
	require.NoError(t, err)

	res, err := ExtractTree(context.Background(), archive, "game.tar.gz", root, TreeOptions{})

	require.NoError(t, err)
	assert.Equal(t, int64(1), res.FilesProcessed)
	assert.Equal(t, map[string]string{"game_file_from_tar_gz": "game_file_from_tar_gz\n"}, readTree(t, dir))
}

func TestExtractTree_SingleCompressedFile(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "server.jar.gz")

	f, err := os.Create(archivePath)
	require.NoError(t, err)

	zw := gzip.NewWriter(f)
	_, err = zw.Write([]byte("jar content"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	root, dir := treeDestination(t)

	res, err := ExtractTree(context.Background(), openArchive(t, archivePath), "server.jar.gz", root, TreeOptions{})

	require.NoError(t, err)
	assert.Equal(t, pb.ArchiveFormat_ARCHIVE_FORMAT_GZ, res.Format)
	assert.Equal(t, int64(1), res.FilesProcessed)
	assert.Equal(t, map[string]string{"server.jar": "jar content"}, readTree(t, dir))
}

func TestExtractTree_EmptyArchive(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "empty.zip")
	buildZip(t, archivePath, nil)

	root, dir := treeDestination(t)

	res, err := ExtractTree(context.Background(), openArchive(t, archivePath), "empty.zip", root, TreeOptions{})

	require.NoError(t, err)
	assert.Zero(t, res.FilesProcessed, "the caller tells an archive that held nothing by the count")
	assert.Empty(t, readTree(t, dir))
}

func TestExtractTree_UnreadableArchive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		file      string
		content   string
		wantError string
	}{
		{
			name:      "content_is_no_archive_and_the_name_says_nothing",
			file:      "download",
			content:   "<html>404 Not Found</html>",
			wantError: `cannot detect the format of archive "download"`,
		},
		{
			name:      "content_is_not_what_the_name_says",
			file:      "game.zip",
			content:   "<html>404 Not Found</html>",
			wantError: "failed to read zip archive",
		},
		{
			name:      "truncated_archive",
			file:      "game.tar.gz",
			content:   "\x1f\x8b\x08\x00",
			wantError: "EOF",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			archivePath := filepath.Join(t.TempDir(), tt.file)
			require.NoError(t, os.WriteFile(archivePath, []byte(tt.content), 0o644))

			root, dir := treeDestination(t)

			res, err := ExtractTree(context.Background(), openArchive(t, archivePath), tt.file, root, TreeOptions{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Nil(t, res)
			assert.Empty(t, readTree(t, dir))
		})
	}
}

func TestExtractTree_EntryNamesLeavingTheDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		entry     string
		wantError string
	}{
		{
			name:      "parent_directory",
			entry:     "../evil.txt",
			wantError: `archive entry "../evil.txt" escapes the destination`,
		},
		{
			name:      "parent_directory_behind_a_directory",
			entry:     "maps/../../evil.txt",
			wantError: `archive entry "maps/../../evil.txt" escapes the destination`,
		},
		{
			name:      "absolute_name",
			entry:     "/etc/evil.txt",
			wantError: `archive entry "/etc/evil.txt" escapes the destination`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parent := t.TempDir()
			dir := filepath.Join(parent, "server")
			require.NoError(t, os.Mkdir(dir, 0o755))

			root, err := os.OpenRoot(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })

			archivePath := filepath.Join(t.TempDir(), "game.zip")
			buildZip(t, archivePath, map[string]string{tt.entry: "evil"})

			res, err := ExtractTree(context.Background(), openArchive(t, archivePath), "game.zip", root, TreeOptions{})

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			assert.Nil(t, res)
			assert.Empty(t, readTree(t, parent))
		})
	}
}

// Extract caps what a panel user may unpack. An installation unpacks what the
// operator configured, and a game is allowed more files than that.
func TestExtractTree_FileLimitIsNotApplied(t *testing.T) {
	t.Parallel()

	entries := int(defaultMaxFiles) + 1

	archivePath := filepath.Join(t.TempDir(), "many.zip")

	f, err := os.Create(archivePath)
	require.NoError(t, err)

	zw := zip.NewWriter(f)
	for range entries {
		_, err := zw.Create("maps/")
		require.NoError(t, err)
	}

	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	root, dir := treeDestination(t)

	res, err := ExtractTree(context.Background(), openArchive(t, archivePath), "many.zip", root, TreeOptions{})

	require.NoError(t, err)
	assert.Equal(t, int64(entries), res.FilesProcessed)
	assert.DirExists(t, filepath.Join(dir, "maps"))
}

func TestExtractTree_CanceledContext(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	res, err := ExtractTree(
		canceledContext(t),
		openArchive(t, fixturePath("local_repository", "game.zip")),
		"game.zip",
		root,
		TreeOptions{},
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, res)
	assert.Empty(t, readTree(t, dir))
}
