package gameservercommands

import (
	"os"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

var (
	errProcessWorkDirMissing = errors.New("server process work directory does not exist")
	errProcessWorkDirNotDir  = errors.New("server process work directory is not a directory")
)

// checkProcessWorkDir refuses to start a server whose configured work_dir cannot
// be entered. Without it every process manager would report a missing directory
// differently, or not at all: the executor falls back to the node work directory
// and tmux to the user's home, so the game would quietly start in the wrong
// place. A server without work_dir is not checked, which keeps the historical
// behaviour for everything that runs in the server directory itself.
func checkProcessWorkDir(cfg *config.Config, server *domain.Server) error {
	rel, err := server.ProcessWorkDirRel()
	if err != nil {
		return err
	}

	if rel == "." {
		return nil
	}

	dir, err := server.ProcessWorkDir(cfg)
	if err != nil {
		return err
	}

	info, err := os.Stat(dir)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return errors.WithMessagef(errProcessWorkDirMissing, "%s (work_dir %q)", dir, rel)
	case err != nil:
		return errors.Wrapf(err, "failed to check server process work directory %s", dir)
	case !info.IsDir():
		return errors.WithMessagef(errProcessWorkDirNotDir, "%s (work_dir %q)", dir, rel)
	}

	return nil
}
