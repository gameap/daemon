//go:build linux
// +build linux

package sys

import (
	"os"
	"os/user"
	"path/filepath"

	"github.com/pkg/errors"
)

// https://github.com/gutengo/fil/blob/6109b2e0b5cfdefdef3a254cc1a3eaa35bc89284/file.go#L27
func ChownR(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err == nil {
			err = os.Chown(name, uid, gid)
		}
		return err
	})
}

func IsRootUser() (bool, error) {
	currentUser, err := user.Current()
	if err != nil {
		return false, errors.WithMessage(err, "failed to check current user")
	}

	return currentUser.Username == "root", nil
}
