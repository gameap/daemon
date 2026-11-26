//go:build linux || darwin

package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tarEntry is one entry for buildTarGz. The zero typeflag is a regular file
// and the zero mode 0644.
type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	body     string
	linkname string
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
		if hdr.Mode == 0 {
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
