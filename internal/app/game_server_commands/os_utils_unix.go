//go:build linux || darwin
// +build linux darwin

package gameservercommands

import (
	"os"
	"os/user"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// https://github.com/gutengo/fil/blob/6109b2e0b5cfdefdef3a254cc1a3eaa35bc89284/file.go#L27
func chownR(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			// Ignore invalid
			return nil
		}

		if info.Mode()&os.ModeSymlink != 0 {
			symlinkFile, err := os.Readlink(name)
			if err != nil {
				// Ignore invalid symlink
				return nil
			}

			if _, err = os.Stat(symlinkFile); err != nil {
				// Ignore invalid symlink
				return nil
			}
		}

		return os.Chown(name, uid, gid)
	})
}

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")
		return false
	}
	return currentUser.Username == "root"
}
