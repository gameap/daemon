//go:build linux || darwin

package gameservercommands

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

func chownR(path string, uid, gid int) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()

	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			// Ignore invalid
			return nil
		}
		return root.Lchown(name, uid, gid)
	})
}

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
