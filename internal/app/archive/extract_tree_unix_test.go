//go:build linux || darwin

package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/gameap/gameap/pkg/proto"
)

// tarEntry is one entry for buildTarGz. The zero typeflag is a regular file
// and the zero mode 0644.
type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     string
	linkname string
	// noMode stores the entry without any permission bits, the way a program
	// that never sets them writes its archives.
	noMode bool
}

// buildTarGz writes a tar.gz with the entries in the order given, the way a
// game archive stores them: an entry may rely on the ones before it.
func buildTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "game.tar.gz")

	f, err := os.Create(archivePath)
	require.NoError(t, err)

	zw := gzip.NewWriter(f)
	tw := tar.NewWriter(zw)

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Mode:     e.mode,
			Linkname: e.linkname,
			Size:     int64(len(e.body)),
		}
		if hdr.Typeflag == 0 {
			hdr.Typeflag = tar.TypeReg
		}
		if hdr.Mode == 0 && !e.noMode {
			hdr.Mode = 0o644
		}

		require.NoError(t, tw.WriteHeader(hdr))

		_, err := tw.Write([]byte(e.body))
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())

	return archivePath
}

// keepRemovable gives the directories below dir back to their owner once the
// test is over. The temporary directory of a test is removed as the user the
// test runs as, who cannot empty a directory that is closed to its owner.
func keepRemovable(t *testing.T, dir string) {
	t.Helper()

	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o755)
			}

			return nil
		})
	})
}

// requireClosedDirToStayClosed skips a test that relies on a directory closed
// to its owner (0555) to keep what it holds. It does not everywhere: root takes
// a file out of any directory, and so does everybody on a file system that
// stores the modes without going by them, as the shared folders of a virtual
// machine do.
func requireClosedDirToStayClosed(t *testing.T) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "closed")
	file := filepath.Join(dir, "file")

	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if os.Remove(file) == nil {
		t.Skip("a file is taken out of a directory closed to its owner here")
	}
}

// outsideFile creates a file that stands for something of the node no archive
// may reach, and returns the directory holding it and its path.
func outsideFile(t *testing.T) (dir, file string) {
	t.Helper()

	dir = t.TempDir()
	file = filepath.Join(dir, "passwd")
	require.NoError(t, os.WriteFile(file, []byte("root:x:0:0"), 0o600))

	return dir, file
}

// assertOutsideUntouched checks that the directory outsideFile created holds
// what it held: the one file, with its content and mode.
func assertOutsideUntouched(t *testing.T, dir, file string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing may be created outside of the destination")

	content, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "root:x:0:0", string(content))

	info, err := os.Lstat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode(), "the file outside keeps its type and mode")
}

func assertRegularFile(t *testing.T, path, wantContent string, wantPerm os.FileMode) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "%s has to be a regular file, it is %s", path, info.Mode())
	assert.Equal(t, wantPerm, info.Mode().Perm())

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, wantContent, string(content))
}

func assertSymlink(t *testing.T, path, wantTarget string) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "%s has to be a symlink, it is %s", path, info.Mode())

	target, err := os.Readlink(path)
	require.NoError(t, err)
	assert.Equal(t, wantTarget, target)
}

// A game server writes into its own directory, and an installation unpacks
// into it with the privileges of the daemon. Whatever the server left under
// the name of an archive entry is replaced by the entry: the write never goes
// to where a link there leads.
func TestExtractTree_FileEntryMeetsWhatStandsUnderItsName(t *testing.T) {
	t.Parallel()

	archive := []tarEntry{
		{name: "./"},
		{name: "./server.cfg", body: "hostname from archive", mode: 0o640},
	}
	archive[0].typeflag = tar.TypeDir
	archive[0].mode = 0o755

	tests := []struct {
		name string
		// plant puts something under the name of the entry before the archive
		// is unpacked.
		plant     func(t *testing.T, dir, outside string)
		wantError string
		check     func(t *testing.T, dir string)
	}{
		{
			name:  "nothing_stands_there",
			plant: func(_ *testing.T, _, _ string) {},
		},
		{
			name: "file_is_replaced",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.WriteFile(filepath.Join(dir, "server.cfg"), []byte("old and much longer content"), 0o600))
			},
		},
		{
			name: "symlink_to_a_file_outside_is_replaced",
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				require.NoError(t, os.Symlink(outside, filepath.Join(dir, "server.cfg")))
			},
		},
		{
			name: "relative_symlink_to_a_file_outside_is_replaced",
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				rel, err := filepath.Rel(dir, outside)
				require.NoError(t, err)
				require.NoError(t, os.Symlink(rel, filepath.Join(dir, "server.cfg")))
			},
		},
		{
			name: "symlink_to_a_missing_file_outside_is_replaced",
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				require.NoError(t, os.Symlink(filepath.Join(filepath.Dir(outside), "created"), filepath.Join(dir, "server.cfg")))
			},
		},
		{
			name: "symlink_to_a_file_inside_is_replaced",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.WriteFile(filepath.Join(dir, "banned.cfg"), []byte("banid 1"), 0o600))
				require.NoError(t, os.Symlink("banned.cfg", filepath.Join(dir, "server.cfg")))
			},
			check: func(t *testing.T, dir string) {
				t.Helper()

				assertRegularFile(t, filepath.Join(dir, "banned.cfg"), "banid 1", 0o600)
			},
		},
		{
			name: "directory_is_kept_and_the_run_stops",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "server.cfg"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "server.cfg", "keep.txt"), []byte("keep"), 0o644))
			},
			wantError: `"server.cfg" is a directory, and the archive has an entry under that name that is not one`,
			check: func(t *testing.T, dir string) {
				t.Helper()

				assertRegularFile(t, filepath.Join(dir, "server.cfg", "keep.txt"), "keep", 0o644)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			outsideDir, outside := outsideFile(t)
			tt.plant(t, dir, outside)

			res, err := ExtractTree(
				context.Background(), openArchive(t, buildTarGz(t, archive)), "game.tar.gz", root, TreeOptions{},
			)

			assertOutsideUntouched(t, outsideDir, outside)

			if tt.check != nil {
				tt.check(t, dir)
			}

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Nil(t, res)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, int64(1), res.FilesProcessed)
			assert.Empty(t, res.SkippedLinks)
			assertRegularFile(t, filepath.Join(dir, "server.cfg"), "hostname from archive", 0o640)
		})
	}
}

// A directory of the archive may meet a symlink the game server put in its
// place. Followed by its path, such a link takes every file below it wherever
// it points.
func TestExtractTree_DirectoryMeetsASymlink(t *testing.T) {
	t.Parallel()

	withDirEntry := []tarEntry{
		{name: "cstrike/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cstrike/addons/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cstrike/addons/plugins.ini", body: "plugins"},
	}
	withoutDirEntry := []tarEntry{
		{name: "cstrike/addons/plugins.ini", body: "plugins"},
	}

	tests := []struct {
		name string
		// plant replaces cstrike/addons, or cstrike, with a symlink.
		plant     func(t *testing.T, dir, outsideDir string)
		wantError string
		wantFile  string
	}{
		{
			name: "link_out_of_the_destination_stops_the_run",
			plant: func(t *testing.T, dir, outsideDir string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink(outsideDir, filepath.Join(dir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "relative_link_out_of_the_destination_stops_the_run",
			plant: func(t *testing.T, dir, outsideDir string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "cstrike"), 0o755))

				rel, err := filepath.Rel(filepath.Join(dir, "cstrike"), outsideDir)
				require.NoError(t, err)
				require.NoError(t, os.Symlink(rel, filepath.Join(dir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_out_of_the_destination_on_the_way_stops_the_run",
			plant: func(t *testing.T, dir, outsideDir string) {
				t.Helper()

				require.NoError(t, os.Symlink(outsideDir, filepath.Join(dir, "cstrike")))
			},
			wantError: `"cstrike" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_to_nowhere_stops_the_run",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink("absent", filepath.Join(dir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that is not followed inside the destination`,
		},
		{
			name: "link_to_a_file_stops_the_run",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "cstrike"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "cstrike", "file"), []byte("file"), 0o644))
				require.NoError(t, os.Symlink("file", filepath.Join(dir, "cstrike", "addons")))
			},
			wantError: `"cstrike/addons" is a symlink that does not lead to a directory`,
		},
		{
			name: "link_to_a_directory_inside_is_merged_through",
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.MkdirAll(filepath.Join(dir, "shared", "addons"), 0o755))
				require.NoError(t, os.Mkdir(filepath.Join(dir, "cstrike"), 0o755))
				require.NoError(t, os.Symlink("../shared/addons", filepath.Join(dir, "cstrike", "addons")))
			},
			wantFile: "shared/addons/plugins.ini",
		},
	}

	archives := map[string][]tarEntry{
		"archive_names_the_directory":        withDirEntry,
		"archive_names_only_the_file_inside": withoutDirEntry,
	}

	for _, tt := range tests {
		for archiveName, archive := range archives {
			t.Run(tt.name+"/"+archiveName, func(t *testing.T) {
				t.Parallel()

				root, dir := treeDestination(t)
				outsideDir, outside := outsideFile(t)
				tt.plant(t, dir, outsideDir)

				res, err := ExtractTree(
					context.Background(), openArchive(t, buildTarGz(t, archive)), "game.tar.gz", root, TreeOptions{},
				)

				assertOutsideUntouched(t, outsideDir, outside)

				if tt.wantError != "" {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tt.wantError)
					assert.Nil(t, res)
					assert.NoFileExists(t, filepath.Join(dir, "cstrike", "absent"))

					return
				}

				require.NoError(t, err)
				assertRegularFile(t, filepath.Join(dir, filepath.FromSlash(tt.wantFile)), "plugins", 0o644)
				assertSymlink(t, filepath.Join(dir, "cstrike", "addons"), "../shared/addons")
			})
		}
	}
}

func TestExtractTree_DirectoryMeetsAFile(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "maps"), []byte("not a directory"), 0o644))

	archive := buildTarGz(t, []tarEntry{
		{name: "maps/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "maps/de_dust2.bsp", body: "bsp"},
	})

	res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

	require.NoError(t, err)
	assert.Equal(t, int64(2), res.FilesProcessed)
	assertRegularFile(t, filepath.Join(dir, "maps", "de_dust2.bsp"), "bsp", 0o644)
}

// A symlink of the archive is created as a symlink. go-getter, which unpacked
// installations before, wrote an empty file with the mode of the link in its
// place — 0777 — through whatever stood under its name.
func TestExtractTree_SymlinkEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		link tarEntry
		// plant puts something into the destination before the archive is
		// unpacked.
		plant       func(t *testing.T, dir, outside string)
		wantLink    string
		wantSkipped []SkippedLink
		wantError   string
	}{
		{
			name:     "link_inside_the_destination_is_created",
			link:     tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "libSDL2-2.0.so.0"},
			wantLink: "libSDL2-2.0.so.0",
		},
		{
			name: "link_replaces_a_symlink_out_of_the_destination",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "libSDL2-2.0.so.0"},
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				require.NoError(t, os.Symlink(outside, filepath.Join(dir, "libSDL2.so")))
			},
			wantLink: "libSDL2-2.0.so.0",
		},
		{
			name: "link_replaces_a_symlink_to_a_missing_file_outside",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "libSDL2-2.0.so.0"},
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				preload := filepath.Join(filepath.Dir(outside), "ld.so.preload")
				require.NoError(t, os.Symlink(preload, filepath.Join(dir, "libSDL2.so")))
			},
			wantLink: "libSDL2-2.0.so.0",
		},
		{
			name: "link_replaces_a_file",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "libSDL2-2.0.so.0"},
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.WriteFile(filepath.Join(dir, "libSDL2.so"), nil, 0o777))
			},
			wantLink: "libSDL2-2.0.so.0",
		},
		{
			name: "absolute_link_is_left_out_and_reported",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "/usr/lib/libSDL2.so"},
			wantSkipped: []SkippedLink{
				{Name: "./libSDL2.so", Target: "/usr/lib/libSDL2.so", Reason: "absolute symlink target"},
			},
		},
		{
			name: "link_out_of_the_destination_is_left_out_and_reported",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "../shared/libSDL2.so"},
			wantSkipped: []SkippedLink{
				{Name: "./libSDL2.so", Target: "../shared/libSDL2.so", Reason: "symlink target escapes the destination"},
			},
		},
		{
			name: "link_through_an_absolute_symlink_of_the_tree_is_left_out_and_reported",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "lib/libSDL2.so"},
			plant: func(t *testing.T, dir, outside string) {
				t.Helper()

				require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(dir, "lib")))
			},
			wantSkipped: []SkippedLink{
				{Name: "./libSDL2.so", Target: "lib/libSDL2.so", Reason: "symlink target crosses an absolute symlink"},
			},
		},
		{
			name: "directory_under_its_name_is_kept_and_the_run_stops",
			link: tarEntry{name: "./libSDL2.so", typeflag: tar.TypeSymlink, mode: 0o777, linkname: "libSDL2-2.0.so.0"},
			plant: func(t *testing.T, dir, _ string) {
				t.Helper()

				require.NoError(t, os.Mkdir(filepath.Join(dir, "libSDL2.so"), 0o755))
			},
			wantError: `"libSDL2.so" is a directory, and the archive has an entry under that name that is not one`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			outsideDir, outside := outsideFile(t)

			if tt.plant != nil {
				tt.plant(t, dir, outside)
			}

			archive := buildTarGz(t, []tarEntry{
				{name: "./", typeflag: tar.TypeDir, mode: 0o755},
				tt.link,
				{name: "./libSDL2-2.0.so.0", body: "ELF", mode: 0o755},
			})

			res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

			assertOutsideUntouched(t, outsideDir, outside)

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Nil(t, res)
				assert.DirExists(t, filepath.Join(dir, "libSDL2.so"))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSkipped, res.SkippedLinks)
			assertRegularFile(t, filepath.Join(dir, "libSDL2-2.0.so.0"), "ELF", 0o755)

			if tt.wantLink == "" {
				assert.Equal(t, int64(1), res.FilesProcessed)
				assert.NoFileExists(t, filepath.Join(dir, "libSDL2.so"), "a link that was left out leaves no file behind")

				return
			}

			assert.Equal(t, int64(2), res.FilesProcessed)
			assertSymlink(t, filepath.Join(dir, "libSDL2.so"), tt.wantLink)
		})
	}
}

// The archive is unpacked with the privileges of the daemon, and the tree
// goes to the game server user. A file keeps the permission bits the archive
// has for it and nothing above them: a setuid bit must not survive that.
func TestExtractTree_FilePermissions(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	archive := buildTarGz(t, []tarEntry{
		{name: "hlds_run", body: "#!/bin/sh", mode: 0o755},
		{name: "steam_appid.txt", body: "90", mode: 0o644},
		{name: "private.key", body: "key", mode: 0o600},
		{name: "setuid_tool", body: "ELF", mode: 0o4755},
		{name: "setgid_sticky_tool", body: "ELF", mode: 0o3755},
	})

	res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

	require.NoError(t, err)
	assert.Equal(t, int64(5), res.FilesProcessed)

	wantModes := map[string]os.FileMode{
		"hlds_run":           0o755,
		"steam_appid.txt":    0o644,
		"private.key":        0o600,
		"setuid_tool":        0o755,
		"setgid_sticky_tool": 0o755,
	}

	for name, want := range wantModes {
		info, err := os.Lstat(filepath.Join(dir, name))
		require.NoError(t, err, name)
		assert.Equal(t, want, info.Mode(), name)
	}
}

// A directory a tar lists is as open or as closed as the archive has it. The
// archives of AMX Mod X keep addons/amxmodx at 0700 over files of 0664:
// users.ini and sql.cfg hold passwords, and the directory is all that keeps
// them from the other users of the node.
func TestExtractTree_DirectoryModes(t *testing.T) {
	t.Parallel()

	const destination = "."

	closedDir := []tarEntry{
		{name: "amxmodx/", typeflag: tar.TypeDir, mode: 0o700},
		{name: "amxmodx/users.ini", body: "admin", mode: 0o664},
	}

	tarOf := func(entries ...tarEntry) func(t *testing.T) string {
		return func(t *testing.T) string {
			t.Helper()

			return buildTarGz(t, entries)
		}
	}

	tests := []struct {
		name string
		// setup prepares the destination before the archive is unpacked.
		setup func(t *testing.T, dir string)
		// archive builds the archive and returns its path.
		archive func(t *testing.T) string
		// wantModes maps the directories to look at to their permission bits.
		wantModes map[string]os.FileMode
	}{
		{
			name:    "tar_directory_takes_the_mode_of_its_entry",
			archive: tarOf(closedDir...),
			wantModes: map[string]os.FileMode{
				"amxmodx": 0o700,
			},
		},
		{
			name: "tar_directory_open_to_the_group_or_to_everyone_stays_so",
			archive: tarOf(
				tarEntry{name: "cstrike/", typeflag: tar.TypeDir, mode: 0o775},
				tarEntry{name: "cstrike/SAVE/", typeflag: tar.TypeDir, mode: 0o744},
				tarEntry{name: "upload/", typeflag: tar.TypeDir, mode: 0o777},
			),
			wantModes: map[string]os.FileMode{
				"cstrike":      0o775,
				"cstrike/SAVE": 0o744,
				"upload":       0o777,
			},
		},
		{
			name:    "tar_directory_listed_after_the_files_in_it",
			archive: tarOf(closedDir[1], closedDir[0]),
			wantModes: map[string]os.FileMode{
				"amxmodx": 0o700,
			},
		},
		{
			name: "tar_directory_that_was_there_takes_the_mode_too",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, "amxmodx"), 0o755))
			},
			archive: tarOf(closedDir...),
			wantModes: map[string]os.FileMode{
				"amxmodx": 0o700,
			},
		},
		{
			name:    "tar_special_bits_of_a_directory_are_left_out",
			archive: tarOf(tarEntry{name: "shared/", typeflag: tar.TypeDir, mode: 0o3775}),
			wantModes: map[string]os.FileMode{
				"shared": 0o775,
			},
		},
		{
			name:    "tar_directory_only_implied_is_created_with_0755",
			archive: tarOf(tarEntry{name: "implied/by/file.txt", body: "file", mode: 0o600}),
			wantModes: map[string]os.FileMode{
				"implied":    0o755,
				"implied/by": 0o755,
			},
		},
		{
			name: "tar_entry_for_the_destination_gives_it_its_mode",
			archive: tarOf(
				tarEntry{name: "./", typeflag: tar.TypeDir, mode: 0o700},
				tarEntry{name: "./hlds_run", body: "#!/bin/sh", mode: 0o755},
				tarEntry{name: "./cstrike/", typeflag: tar.TypeDir, mode: 0o755},
			),
			wantModes: map[string]os.FileMode{
				destination: 0o700,
				"cstrike":   0o755,
			},
		},
		{
			name: "tar_without_an_entry_for_the_destination_leaves_it_as_it_is",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Chmod(dir, 0o750))
			},
			archive: tarOf(closedDir...),
			wantModes: map[string]os.FileMode{
				destination: 0o750,
				"amxmodx":   0o700,
			},
		},
		{
			name: "tar_directory_below_one_only_implied_leaves_that_one_at_0755",
			archive: tarOf(
				tarEntry{name: "implied/closed/", typeflag: tar.TypeDir, mode: 0o700},
				tarEntry{name: "implied/closed/users.ini", body: "admin", mode: 0o664},
			),
			wantModes: map[string]os.FileMode{
				"implied":        0o755,
				"implied/closed": 0o700,
			},
		},
		{
			name: "tar_directory_without_permission_bits_is_created_with_0755",
			archive: tarOf(
				tarEntry{name: "generated/", typeflag: tar.TypeDir, noMode: true},
				tarEntry{name: "generated/file.txt", body: "file", noMode: true},
			),
			wantModes: map[string]os.FileMode{
				"generated": 0o755,
			},
		},
		{
			name: "tar_directory_that_was_there_keeps_its_mode_under_an_entry_without_permission_bits",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0o700))
			},
			archive: tarOf(
				tarEntry{name: "logs/", typeflag: tar.TypeDir, noMode: true},
				tarEntry{name: "logs/server.log", body: "log", noMode: true},
			),
			wantModes: map[string]os.FileMode{
				"logs": 0o700,
			},
		},
		{
			name: "tar_entry_for_the_destination_without_permission_bits_leaves_it_as_it_is",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Chmod(dir, 0o750))
			},
			archive: tarOf(
				tarEntry{name: "./", typeflag: tar.TypeDir, noMode: true},
				tarEntry{name: "./hlds_run", body: "#!/bin/sh", mode: 0o755},
			),
			wantModes: map[string]os.FileMode{
				destination: 0o750,
			},
		},
		{
			name: "zip_directory_is_created_with_0755_whatever_the_archive_has",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0o700))
			},
			archive: func(t *testing.T) string {
				t.Helper()

				archivePath := filepath.Join(t.TempDir(), "game.zip")
				buildZipModes(t, archivePath, []zipEntry{
					{name: "./", mode: os.ModeDir | 0o700},
					{name: "open/", mode: os.ModeDir | 0o777},
					{name: "closed/", mode: os.ModeDir | 0o700},
					{name: "logs/", mode: os.ModeDir | 0o777},
					{name: "open/file.txt", mode: 0o644, body: "file"},
				})

				return archivePath
			},
			wantModes: map[string]os.FileMode{
				destination: 0o755,
				"open":      0o755,
				"closed":    0o755,
				"logs":      0o700,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			require.NoError(t, os.Chmod(dir, 0o755))

			if tt.setup != nil {
				tt.setup(t, dir)
			}

			archive := tt.archive(t)

			_, err := ExtractTree(context.Background(), openArchive(t, archive), filepath.Base(archive), root, TreeOptions{})
			require.NoError(t, err)

			for name, want := range tt.wantModes {
				info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
				require.NoError(t, err, name)
				require.True(t, info.IsDir(), name)
				assert.Equal(t, want, info.Mode()&^os.ModeDir, name)
			}
		})
	}
}

// An archive can keep a directory from being written to by its own owner:
// 0555 is how some games ship theirs. The entries of such a directory come
// after its own, and a daemon that is not root creates them only as long as
// the directory can still be written to. It takes its mode once they are in.
func TestExtractTree_DirectoryClosedToItsOwner(t *testing.T) {
	t.Parallel()

	const destination = "."

	// A directory of the server that a symlink of its own leads to: an archive
	// may list it under either name.
	linkedDir := func(t *testing.T, dir string) {
		t.Helper()

		require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
		require.NoError(t, os.Symlink("real", filepath.Join(dir, "link")))
	}

	tests := []struct {
		name string
		// setup prepares the destination before the archive is unpacked.
		setup     func(t *testing.T, dir string)
		entries   []tarEntry
		wantModes map[string]os.FileMode
		wantFiles map[string]string
	}{
		{
			name: "directory_takes_its_entries_first",
			entries: []tarEntry{
				{name: "assets/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "assets/models/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "assets/models/player.mdl", body: "mdl", mode: 0o444},
				{name: "assets/readme.txt", body: "readme"},
			},
			wantModes: map[string]os.FileMode{
				"assets":        0o555,
				"assets/models": 0o555,
			},
			wantFiles: map[string]string{
				"assets/models/player.mdl": "mdl",
				"assets/readme.txt":        "readme",
			},
		},
		{
			name: "directory_that_was_there_takes_the_entries_of_an_update",
			setup: func(t *testing.T, dir string) {
				t.Helper()

				assets := filepath.Join(dir, "assets")
				require.NoError(t, os.Mkdir(assets, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(assets, "readme.txt"), []byte("the previous version"), 0o644))
				require.NoError(t, os.WriteFile(filepath.Join(assets, "kept.txt"), []byte("kept"), 0o644))
				require.NoError(t, os.Chmod(assets, 0o555))
			},
			entries: []tarEntry{
				{name: "assets/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "assets/readme.txt", body: "readme"},
				{name: "assets/added.txt", body: "added"},
			},
			wantModes: map[string]os.FileMode{
				"assets": 0o555,
			},
			wantFiles: map[string]string{
				"assets/readme.txt": "readme",
				"assets/added.txt":  "added",
				"assets/kept.txt":   "kept",
			},
		},
		{
			name: "directory_listed_twice_ends_up_with_the_later_mode",
			entries: []tarEntry{
				{name: "assets/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "assets/readme.txt", body: "readme"},
				{name: "assets/", typeflag: tar.TypeDir, mode: 0o750},
			},
			wantModes: map[string]os.FileMode{
				"assets": 0o750,
			},
			wantFiles: map[string]string{
				"assets/readme.txt": "readme",
			},
		},
		{
			name:  "directory_listed_under_two_names_ends_up_with_the_later_mode",
			setup: linkedDir,
			entries: []tarEntry{
				{name: "link/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "link/readme.txt", body: "readme"},
				{name: "real/", typeflag: tar.TypeDir, mode: 0o750},
			},
			wantModes: map[string]os.FileMode{
				"real": 0o750,
			},
			wantFiles: map[string]string{
				"real/readme.txt": "readme",
			},
		},
		{
			name:  "directory_listed_under_two_names_is_closed_by_the_later_one",
			setup: linkedDir,
			entries: []tarEntry{
				{name: "real/", typeflag: tar.TypeDir, mode: 0o750},
				{name: "link/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "real/readme.txt", body: "readme"},
			},
			wantModes: map[string]os.FileMode{
				"real": 0o555,
			},
			wantFiles: map[string]string{
				"real/readme.txt": "readme",
			},
		},
		{
			name:  "directory_below_one_listed_under_two_names",
			setup: linkedDir,
			entries: []tarEntry{
				{name: "link/assets/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "real/assets/", typeflag: tar.TypeDir, mode: 0o750},
				{name: "link/assets/readme.txt", body: "readme"},
			},
			wantModes: map[string]os.FileMode{
				"real":        0o755,
				"real/assets": 0o750,
			},
			wantFiles: map[string]string{
				"real/assets/readme.txt": "readme",
			},
		},
		{
			name: "directory_below_one_only_implied",
			entries: []tarEntry{
				{name: "implied/assets/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "implied/assets/readme.txt", body: "readme"},
			},
			wantModes: map[string]os.FileMode{
				"implied":        0o755,
				"implied/assets": 0o555,
			},
			wantFiles: map[string]string{
				"implied/assets/readme.txt": "readme",
			},
		},
		{
			name: "destination_takes_its_entries_first",
			entries: []tarEntry{
				{name: "./", typeflag: tar.TypeDir, mode: 0o555},
				{name: "./hlds_run", body: "#!/bin/sh", mode: 0o755},
				{name: "./cstrike/", typeflag: tar.TypeDir, mode: 0o755},
				{name: "./cstrike/server.cfg", body: "hostname"},
			},
			wantModes: map[string]os.FileMode{
				destination: 0o555,
				"cstrike":   0o755,
			},
			wantFiles: map[string]string{
				"hlds_run":           "#!/bin/sh",
				"cstrike/server.cfg": "hostname",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			require.NoError(t, os.Chmod(dir, 0o755))
			keepRemovable(t, dir)

			if tt.setup != nil {
				tt.setup(t, dir)
			}

			archive := buildTarGz(t, tt.entries)

			_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})
			require.NoError(t, err)

			for name, want := range tt.wantModes {
				info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
				require.NoError(t, err, name)
				require.True(t, info.IsDir(), name)
				assert.Equal(t, want, info.Mode().Perm(), name)
			}

			for name, want := range tt.wantFiles {
				content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
				require.NoError(t, err, name)
				assert.Equal(t, want, string(content), name)
			}
		})
	}
}

// The modes held back are applied from the deepest directory up. A directory
// is reached through the ones above it, and the archive may take the search
// permission from one of those.
func TestExtractTree_DirectoryThatCannotBeSearchedIsClosedLast(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)
	keepRemovable(t, dir)

	archive := buildTarGz(t, []tarEntry{
		{name: "vault/", typeflag: tar.TypeDir, mode: 0o600},
		{name: "vault/inner/", typeflag: tar.TypeDir, mode: 0o500},
		{name: "vault/inner/key", body: "key", mode: 0o400},
	})

	_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})
	require.NoError(t, err)

	vault := filepath.Join(dir, "vault")

	info, err := os.Lstat(vault)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// What is below can only be looked at once the directory lets it.
	require.NoError(t, os.Chmod(vault, 0o700))

	info, err = os.Lstat(filepath.Join(vault, "inner"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
	assertRegularFile(t, filepath.Join(vault, "inner", "key"), "key", 0o400)
}

// A run that fails takes away what it had put where nothing stood, so that the
// next source of the installation does not find it among its own files. What
// was there before stays: a file the run had replaced, as the archive has it,
// and a directory it had merged into, no more open to its owner than the
// archive has it.
func TestExtractTree_FailedRun(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)
	keepRemovable(t, dir)

	assets := filepath.Join(dir, "assets")
	require.NoError(t, os.Mkdir(assets, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(assets, "readme.txt"), []byte("the previous version"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "server.cfg"), []byte("hostname"), 0o600))
	require.NoError(t, os.Symlink("server.cfg", filepath.Join(dir, "kept_link")))

	archive := buildTarGz(t, []tarEntry{
		{name: "assets/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "assets/readme.txt", body: "readme"},
		{name: "assets/added.txt", body: "added"},
		{name: "assets/models/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "assets/models/player.mdl", body: "mdl", mode: 0o444},
		{name: "implied/by/file.txt", body: "file"},
		{name: "hlds_run", body: "#!/bin/sh", mode: 0o755},
		{name: "libSDL2.so", typeflag: tar.TypeSymlink, linkname: "libSDL2-2.0.so.0"},
		{name: "../passwd", body: "root:x:0:0"},
	})

	res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `archive entry "../passwd" escapes the destination`)
	assert.Nil(t, res)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	assert.Equal(t, []string{"assets", "kept_link", "server.cfg"}, names, "nothing new is left in the destination")

	info, err := os.Lstat(assets)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o555), info.Mode().Perm())

	entries, err = os.ReadDir(assets)
	require.NoError(t, err)
	require.Len(t, entries, 1, "nothing new is left in a directory that was there")

	assertRegularFile(t, filepath.Join(assets, "readme.txt"), "readme", 0o644)
	assertRegularFile(t, filepath.Join(dir, "server.cfg"), "hostname", 0o600)
	assertSymlink(t, filepath.Join(dir, "kept_link"), "server.cfg")
}

// A run that is canceled is a run that failed.
func TestExtractTree_CanceledRunLeavesNothingNew(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	archive := buildTarGz(t, []tarEntry{
		{name: "cstrike/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "cstrike/server.cfg", body: "hostname"},
		{name: "hlds_run", body: "#!/bin/sh", mode: 0o755},
	})

	ctx, cancel := context.WithCancel(context.Background())

	// The progress of the first entry is the last thing the run gets to report.
	_, err := ExtractTree(ctx, openArchive(t, archive), "game.tar.gz", root, TreeOptions{
		Progress: func(int64, int64, string) { cancel() },
	})

	require.ErrorIs(t, err, context.Canceled)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A symlink the root follows to a directory takes directory entries, and a
// later entry may take the link away. The directories reached through it stay
// where they are: they go on taking entries under their own names, and get
// their modes once the archive is through.
func TestExtractTree_DirectoriesBehindALinkThatIsReplaced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []tarEntry
		// wantFiles maps the files expected below real/sub to their content.
		wantFiles map[string]string
	}{
		{
			name: "link_is_the_last_the_archive_has_for_them",
			entries: []tarEntry{
				{name: "link/sub/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "link/sub/readme.txt", body: "readme"},
				{name: "link", body: "a file now", mode: 0o600},
			},
			wantFiles: map[string]string{
				"readme.txt": "readme",
			},
		},
		{
			name: "entries_come_under_their_own_names_afterwards",
			entries: []tarEntry{
				{name: "link/sub/", typeflag: tar.TypeDir, mode: 0o555},
				{name: "link", body: "a file now", mode: 0o600},
				{name: "real/sub/readme.txt", body: "readme"},
				{name: "real/sub/models/", typeflag: tar.TypeDir, mode: 0o755},
				{name: "real/sub/models/player.mdl", body: "mdl"},
			},
			wantFiles: map[string]string{
				"readme.txt":        "readme",
				"models/player.mdl": "mdl",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root, dir := treeDestination(t)
			keepRemovable(t, dir)

			require.NoError(t, os.Mkdir(filepath.Join(dir, "real"), 0o755))
			require.NoError(t, os.Symlink("real", filepath.Join(dir, "link")))

			archive := buildTarGz(t, tt.entries)

			_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})
			require.NoError(t, err)

			assertRegularFile(t, filepath.Join(dir, "link"), "a file now", 0o600)

			info, err := os.Lstat(filepath.Join(dir, "real", "sub"))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o555), info.Mode().Perm())

			for name, want := range tt.wantFiles {
				assertRegularFile(t, filepath.Join(dir, "real", "sub", filepath.FromSlash(name)), want, 0o644)
			}
		})
	}
}

// A run that fails takes away what it created, and not what its names lead to
// by then. An entry that came through a symlink lies where the link led when
// it came, and the archive may have pointed the link elsewhere since: to a
// directory with files of the server under the very same names.
func TestExtractTree_FailedRunAfterALinkWasPointedElsewhere(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")

	require.NoError(t, os.Mkdir(first, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(second, "maps"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(second, "settings.cfg"), []byte("of the server"), 0o600))
	require.NoError(t, os.Symlink("first", filepath.Join(dir, "current")))

	archive := buildTarGz(t, []tarEntry{
		{name: "current/settings.cfg", body: "of the archive"},
		{name: "current/maps/de_dust.bsp", body: "bsp"},
		{name: "current/libSDL2.so", typeflag: tar.TypeSymlink, linkname: "libSDL2-2.0.so.0"},
		{name: "current", typeflag: tar.TypeSymlink, linkname: "second"},
		{name: "current/added.cfg", body: "added"},
		{name: "../passwd", body: "root:x:0:0"},
	})

	_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `archive entry "../passwd" escapes the destination`)

	assert.Empty(t, entryNames(t, first), "what the run created is taken away from where it was created")
	assert.Equal(t, []string{"maps", "settings.cfg"}, entryNames(t, second), "what the server had stays")

	assertRegularFile(t, filepath.Join(second, "settings.cfg"), "of the server", 0o600)
	assertSymlink(t, filepath.Join(dir, "current"), "second")
}

// The links of a run that fails over them are gone by the time the rest of it
// is taken away. What came through one of them is found all the same.
func TestExtractTree_FailedRunTakesAwayWhatCameThroughItsOwnLink(t *testing.T) {
	t.Parallel()

	root, dir := treeDestination(t)

	// Each of the last two links stays inside the destination on its own, and
	// they lead out of it together.
	archive := buildTarGz(t, []tarEntry{
		{name: "data/", typeflag: tar.TypeDir, mode: 0o755},
		{name: "link", typeflag: tar.TypeSymlink, linkname: "data"},
		{name: "link/settings.cfg", body: "of the archive"},
		{name: "sub/deep/out", typeflag: tar.TypeSymlink, linkname: "../up/../../passwd"},
		{name: "sub/up", typeflag: tar.TypeSymlink, linkname: ".."},
	})

	_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `symlink "sub/deep/out"`)

	assert.Empty(t, entryNames(t, dir), "nothing new is left in the destination")
}

// entryNames lists what dir holds.
func entryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// The modes of what is unpacked are the ones the package documents, whatever
// umask the daemon was started with. A directory an entry only implies is
// created with 0755 like any other.
func TestExtract_ModesDoNotDependOnTheUmask(t *testing.T) {
	// The umask is that of the process. A test that is not parallel runs while
	// the parallel ones wait, so none of them sees it.
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })

	entries := []tarEntry{
		{name: "implied/by/file.txt", body: "file", mode: 0o664},
		{name: "listed/", typeflag: tar.TypeDir, mode: 0o775},
		{name: "listed/below/a/file.txt", body: "file"},
	}

	wantDirs := map[string]os.FileMode{
		"implied":        0o755,
		"implied/by":     0o755,
		"listed":         0o775,
		"listed/below":   0o755,
		"listed/below/a": 0o755,
	}

	assertModes := func(t *testing.T, dir string) {
		t.Helper()

		for name, want := range wantDirs {
			info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
			require.NoError(t, err, name)
			assert.Equal(t, want, info.Mode().Perm(), name)
		}

		assertRegularFile(t, filepath.Join(dir, "implied", "by", "file.txt"), "file", 0o664)
	}

	t.Run("installation", func(t *testing.T) {
		root, dir := treeDestination(t)
		archive := buildTarGz(t, entries)

		_, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})
		require.NoError(t, err)

		assertModes(t, dir)
	})

	t.Run("request", func(t *testing.T) {
		workDir := t.TempDir()

		archive, err := os.ReadFile(buildTarGz(t, entries))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "game.tar.gz"), archive, 0o644))

		_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
			ArchivePath:         "game.tar.gz",
			Destination:         "servers/dst",
			CreateDestination:   true,
			PreservePermissions: true,
		}, nil)
		require.NoError(t, err)

		for _, created := range []string{"servers", "servers/dst"} {
			info, err := os.Lstat(filepath.Join(workDir, filepath.FromSlash(created)))
			require.NoError(t, err, created)
			assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), created)
		}

		assertModes(t, filepath.Join(workDir, "servers", "dst"))
	})
}

// Extract takes the modes of directories from the archive when the request
// asks for them, and holds them back the same way.
func TestExtract_PreservedDirectoryClosedToItsOwner(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	keepRemovable(t, workDir)

	archive, err := os.ReadFile(buildTarGz(t, []tarEntry{
		{name: "assets/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "assets/models/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "assets/models/player.mdl", body: "mdl", mode: 0o444},
		{name: "replaced/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "replaced/inner/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "replaced", body: "a file now", mode: 0o640},
	}))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "game.tar.gz"), archive, 0o644))

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:         "game.tar.gz",
		Destination:         "dst",
		CreateDestination:   true,
		PreservePermissions: true,
		ConflictPolicy:      pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
	}, nil)
	require.NoError(t, err)

	for _, name := range []string{"assets", "assets/models"} {
		info, err := os.Lstat(filepath.Join(workDir, "dst", filepath.FromSlash(name)))
		require.NoError(t, err, name)
		assert.Equal(t, os.FileMode(0o555), info.Mode().Perm(), name)
	}

	assertRegularFile(t, filepath.Join(workDir, "dst", "assets", "models", "player.mdl"), "mdl", 0o444)
	assertRegularFile(t, filepath.Join(workDir, "dst", "replaced"), "a file now", 0o640)
}

// A directory that makes room for an entry is removed with all below it. When
// that fails, what is left of it is still a directory the archive has a mode
// for, and takes that mode like after any other run that failed.
func TestExtract_DirectoryThatCannotBeRemovedStillTakesItsMode(t *testing.T) {
	t.Parallel()

	requireClosedDirToStayClosed(t)

	workDir := t.TempDir()
	keepRemovable(t, workDir)

	replaced := filepath.Join(workDir, "dst", "replaced")
	kept := filepath.Join(replaced, "kept")

	require.NoError(t, os.MkdirAll(kept, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(kept, "file.txt"), []byte("file"), 0o644))
	require.NoError(t, os.Chmod(kept, 0o555))

	archive, err := os.ReadFile(buildTarGz(t, []tarEntry{
		{name: "replaced/", typeflag: tar.TypeDir, mode: 0o555},
		{name: "replaced", body: "a file now"},
	}))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "game.tar.gz"), archive, 0o644))

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:         "game.tar.gz",
		Destination:         "dst",
		PreservePermissions: true,
		ConflictPolicy:      pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
	}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to remove "dst/replaced"`)

	info, err := os.Lstat(replaced)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	assert.Equal(t, os.FileMode(0o555), info.Mode().Perm())
}

// A directory that was there keeps its mode under an entry that records none,
// also when the request asks for the permissions of the archive.
func TestExtract_PreservedDirectoryWithoutPermissionBits(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()

	logs := filepath.Join(workDir, "dst", "logs")
	require.NoError(t, os.MkdirAll(logs, 0o755))
	require.NoError(t, os.Chmod(logs, 0o700))

	archive, err := os.ReadFile(buildTarGz(t, []tarEntry{
		{name: "logs/", typeflag: tar.TypeDir, noMode: true},
		{name: "logs/server.log", body: "log", noMode: true},
		{name: "added/", typeflag: tar.TypeDir, noMode: true},
	}))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "game.tar.gz"), archive, 0o644))

	_, err = Extract(context.Background(), workDir, &pb.ExtractArchiveParams{
		ArchivePath:         "game.tar.gz",
		Destination:         "dst",
		PreservePermissions: true,
		ConflictPolicy:      pb.ArchiveConflictPolicy_ARCHIVE_CONFLICT_POLICY_OVERWRITE,
	}, nil)
	require.NoError(t, err)

	info, err := os.Lstat(logs)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	info, err = os.Lstat(filepath.Join(workDir, "dst", "added"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
}

// A hardlink of the archive is stored as a copy, and what it copies is read
// through the root like everything else.
func TestExtractTree_HardlinkEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		linkname  string
		wantError string
	}{
		{
			name:     "target_inside_the_archive_is_copied",
			linkname: "engine.so",
		},
		{
			name:      "target_out_of_the_destination_stops_the_run",
			linkname:  "../passwd",
			wantError: `archive entry "../passwd" escapes the destination`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parent := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(parent, "passwd"), []byte("root:x:0:0"), 0o600))

			dir := filepath.Join(parent, "server")
			require.NoError(t, os.Mkdir(dir, 0o755))

			root, err := os.OpenRoot(dir)
			require.NoError(t, err)
			t.Cleanup(func() { _ = root.Close() })

			archive := buildTarGz(t, []tarEntry{
				{name: "engine.so", body: "ELF", mode: 0o755},
				{name: "engine_copy.so", typeflag: tar.TypeLink, mode: 0o755, linkname: tt.linkname},
			})

			res, err := ExtractTree(context.Background(), openArchive(t, archive), "game.tar.gz", root, TreeOptions{})

			if tt.wantError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				assert.Nil(t, res)
				assert.NoFileExists(t, filepath.Join(dir, "engine_copy.so"))

				return
			}

			require.NoError(t, err)
			assert.Equal(t, int64(2), res.FilesProcessed)
			assertRegularFile(t, filepath.Join(dir, "engine_copy.so"), "ELF", 0o755)
		})
	}
}
