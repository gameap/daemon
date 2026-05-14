//go:build linux || darwin

package osowner

import (
	"io/fs"
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

	return currentUser.Username == "root"
}

func lchown(path string, uid, gid int) error {
	return os.Lchown(path, uid, gid)
}

func chownTree(path string, uid, gid int) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()

	return fs.WalkDir(root.FS(), ".", func(name string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		return root.Lchown(name, uid, gid)
	})
}
