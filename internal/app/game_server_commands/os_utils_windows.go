//go:build windows
// +build windows

package gameservercommands

import (
	"os/user"

	log "github.com/sirupsen/logrus"
)

func chownR(_ string, _, _ int) error {
	return nil
}

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")
		return false
	}
	return currentUser.Username == "System" || currentUser.Username == "Administrator"
}
