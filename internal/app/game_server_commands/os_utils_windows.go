//go:build windows

package gameservercommands

import (
	"os"
	"os/user"

	log "github.com/sirupsen/logrus"
)

func chownR(_ string, _, _ int) error {
	return nil
}

func mkdirAllWithFinalPerm(path string, finalPerm os.FileMode) error {
	return os.MkdirAll(path, finalPerm)
}

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")
		return false
	}
	return currentUser.Username == "System" || currentUser.Username == "Administrator"
}
