package processmanager

import (
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

	path, pathErr := lookPathAbs(cmd)
	if pathErr == nil {
		return path, nil
	}

	return "", errors.WithMessagef(
		workDirErr, "failed to find command %q in %q and in PATH", cmd, processWorkDir,
	)
}

// lookPathAbs is exec.LookPath with a result that is always absolute.
//
// Windows searches the calling process's own working directory before PATH, and LookPath
// reports such a hit as exec.ErrDot together with the path it found rather than as a plain
// failure. That path is relative to the daemon's working directory, which has nothing to do
// with the game server, so it is anchored here instead of being handed to a supervisor that
// would resolve it somewhere else entirely.
func lookPathAbs(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return "", errors.Wrapf(err, "failed to look up %q", name)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to make path %q absolute", path)
	}

	return abs, nil
}
