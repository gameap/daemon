//go:build linux || darwin

package gameservercommands

import (
	"os"
	"os/user"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func mkdirAllWithFinalPerm(path string, finalPerm os.FileMode) error {
	parent := filepath.Dir(path)
	if parent != "." && parent != "/" {
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
	}
	return os.Mkdir(path, finalPerm)
}

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")
		return false
	}
	return currentUser.Username == "root"
}
