//go:build linux || darwin

package gameservercommands

import (
	"os/user"

	log "github.com/sirupsen/logrus"
)

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")
		return false
	}
	return currentUser.Username == "root"
}
