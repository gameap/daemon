package processmanager

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pkg/errors"
)

// resolveCommandExecutable returns the absolute path of the program a start command begins
// with, looking for it in the directory the game server process runs in and then in PATH.
//
// Supervisors do not resolve a relative program against the working directory they are told to
// use. systemd expands ExecStart= before it applies WorkingDirectory=, and Windows resolves a
// program that carries a path separator against the directory of the process that requested the
// start, never against the one the service is given. A start command spelled `.\server.exe` or
// `bin\srcds.exe` — the spelling every Linux entry in the games catalogue uses — therefore names
// a path that does not exist, and the game server fails to launch with nothing but the
// supervisor's own "file not found" to go on. An absolute path means the same file for every
// supervisor, whatever directory it happens to start from.
//
// A bare name that the server directory does not hold falls back to PATH, so an interpreter
// such as powershell or java stays reachable.
func resolveCommandExecutable(cmd, processWorkDir string) (string, error) {
	if filepath.IsAbs(cmd) {
		path, err := lookPathAbs(cmd)
		if err != nil {
			return "", errors.WithMessagef(err, "failed to find command %q", cmd)
		}

		return path, nil
	}

	path, workDirErr := lookPathAbs(filepath.Join(processWorkDir, cmd))
	if workDirErr == nil {
		return path, nil
	}

	path, pathErr := lookPathInPATH(cmd)
	if pathErr == nil {
		return path, nil
	}

	return "", errors.WithMessagef(
		workDirErr, "failed to find command %q in %q and in PATH", cmd, processWorkDir,
	)
}

// lookPathAbs is exec.LookPath with a result that is always absolute.
//
// A hit reported as exec.ErrDot is refused rather than used. LookPath returns that sentinel
// when the name resolved inside the calling process's own working directory, which on Windows
// is searched implicitly and before PATH. That directory holds the daemon binary and has
// nothing to do with the game server, so a file dropped next to the daemon must never become
// the program a service is registered with.
func lookPathAbs(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", errors.Wrapf(err, "failed to look up %q", name)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to make path %q absolute", path)
	}

	return abs, nil
}

// lookPathInPATH searches PATH alone for a command name.
//
// exec.LookPath cannot do this on Windows: it looks in the calling process's working directory
// first and returns that hit instead of going on to PATH, so refusing the hit afterwards would
// also lose the interpreter that PATH really does provide. Each PATH entry is therefore joined
// with the name and checked on its own, which keeps LookPath's PATHEXT handling while leaving
// the implicit search of the daemon's own directory out of it.
//
// Entries that are not absolute are skipped. The resolved path is written into a unit or a
// service that a supervisor starts from some other directory, where a path that only means
// something relative to the daemon's working directory would point somewhere else or nowhere.
func lookPathInPATH(cmd string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}

		path, err := exec.LookPath(filepath.Join(dir, cmd))
		if err != nil {
			continue
		}

		return path, nil
	}

	return "", errors.Errorf("failed to find %q in PATH", cmd)
}
