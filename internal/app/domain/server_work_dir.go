package domain

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/gameap/daemon/internal/app/fsutil"
	"github.com/pkg/errors"
)

// Keys that configure the directory the game server process runs in, relative
// to the server directory. They are read from the server vars, the game mod
// metadata and the game metadata, in that order.
const (
	WorkDirKey        = "work_dir"
	WorkDirLinuxKey   = "work_dir_linux"
	WorkDirWindowsKey = "work_dir_windows"
	WorkDirMacOSKey   = "work_dir_macos"
)

// ErrProcessWorkDirNotRelative is returned when a work_dir value is anchored to
// a filesystem root. The process work directory must always be inside the
// server directory.
var ErrProcessWorkDirNotRelative = errors.New("work_dir must be a path relative to the server directory")

// ErrProcessWorkDirInvalidCharacters is returned for values that cannot be
// written safely into a process manager configuration: line breaks would
// inject directives into a systemd unit and '%' is a systemd specifier.
var ErrProcessWorkDirInvalidCharacters = errors.New("work_dir must not contain control characters or '%'")

// ProcessWorkDirRel returns the directory the game server process runs in as a
// clean, slash-separated path relative to the server directory, or "." when
// nothing is configured.
//
// The value is looked up in the server vars, then in the game mod metadata,
// then in the game metadata. At each level the OS-specific key (work_dir_linux,
// work_dir_windows or work_dir_macos) wins over the generic work_dir. Empty and
// non-string values are skipped, so a blank override does not hide a default
// set on a lower level.
func (s *Server) ProcessWorkDirRel() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return resolveProcessWorkDir(runtime.GOOS, s.mergedVars(), s.gameMod.Metadata, s.game.Metadata)
}

// ProcessWorkDir returns the absolute directory the game server process runs
// in: the server directory joined with ProcessWorkDirRel, which is the server
// directory itself when no work_dir is configured.
func (s *Server) ProcessWorkDir(cfg workDirReader) (string, error) {
	rel, err := s.ProcessWorkDirRel()
	if err != nil {
		return "", err
	}

	return filepath.Join(s.WorkDir(cfg), filepath.FromSlash(rel)), nil
}

type processWorkDirSource struct {
	name   string
	lookup func(key string) string
}

func resolveProcessWorkDir(
	goos string,
	vars map[string]string,
	gameModMetadata map[string]any,
	gameMetadata map[string]any,
) (string, error) {
	keys := processWorkDirKeys(goos)

	sources := []processWorkDirSource{
		{name: "server vars", lookup: func(key string) string { return vars[key] }},
		{name: "game mod metadata", lookup: metadataStringLookup(gameModMetadata)},
		{name: "game metadata", lookup: metadataStringLookup(gameMetadata)},
	}

	for _, source := range sources {
		for _, key := range keys {
			value := strings.TrimSpace(source.lookup(key))
			if value == "" {
				continue
			}

			rel, err := normalizeProcessWorkDir(value)
			if err != nil {
				return "", errors.WithMessagef(err, "invalid %s %q from %s", key, value, source.name)
			}

			return rel, nil
		}
	}

	return ".", nil
}

// processWorkDirKeys returns the lookup order for one OS: the OS-specific key
// first, the generic key second. macOS does not fall back to the Linux key
// because macOS builds keep their binaries in different directories.
func processWorkDirKeys(goos string) [2]string {
	switch goos {
	case "windows":
		return [2]string{WorkDirWindowsKey, WorkDirKey}
	case "darwin":
		return [2]string{WorkDirMacOSKey, WorkDirKey}
	default:
		return [2]string{WorkDirLinuxKey, WorkDirKey}
	}
}

func metadataStringLookup(metadata map[string]any) func(key string) string {
	return func(key string) string {
		value, ok := metadata[key].(string)
		if !ok {
			return ""
		}

		return value
	}
}

// normalizeProcessWorkDir turns a configured value into a clean root-relative
// path. Anchored values are rejected before fsutil.RootRel sees them, because
// RootRel strips leading separators and drive letters and would quietly turn
// C:\servers\gb into servers/gb.
func normalizeProcessWorkDir(value string) (string, error) {
	if strings.ContainsFunc(value, unicode.IsControl) || strings.Contains(value, "%") {
		return "", ErrProcessWorkDirInvalidCharacters
	}

	if isAnchoredPath(value) {
		return "", ErrProcessWorkDirNotRelative
	}

	rel, err := fsutil.RootRel(value)
	if err != nil {
		return "", err
	}

	return rel, nil
}

// isAnchoredPath reports whether value starts from a filesystem root on any
// OS: a leading separator, a Windows drive letter or a UNC prefix. The check is
// OS-independent on purpose, so a Windows-style absolute path in
// work_dir_windows is rejected even when the daemon validating it runs on Linux.
func isAnchoredPath(value string) bool {
	if filepath.IsAbs(value) || filepath.VolumeName(value) != "" {
		return true
	}

	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) {
		return true
	}

	return len(value) >= 2 && value[1] == ':' && isASCIILetter(value[0])
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
