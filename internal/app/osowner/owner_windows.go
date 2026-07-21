//go:build windows

package osowner

import (
	"os"
	"os/user"

	log "github.com/sirupsen/logrus"
)

func isRootUser() bool {
	currentUser, err := user.Current()
	if err != nil {
		log.Error("Failed to check current user")

		return false
	}

	return currentUser.Username == "System" || currentUser.Username == "Administrator"
}

func lchown(_ string, _, _ int) error {
	return nil
}

func lchownInRoot(_ *os.Root, _ string, _, _ int) error {
	return nil
}

func chownTree(_ string, _, _ int) error {
	return nil
}

func groupShareTree(_ string, _ int) error {
	return nil
}
