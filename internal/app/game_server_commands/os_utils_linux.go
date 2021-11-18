//go:build linux
// +build linux

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
		if err == nil {
			err = os.Chown(name, uid, gid)
		}
		return err
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
