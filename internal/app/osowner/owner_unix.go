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

func lchownInRoot(root *os.Root, name string, uid, gid int) error {
	return root.Lchown(name, uid, gid)
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

func groupShareTree(path string, gid int) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()

	return fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if d.Type()&fs.ModeSymlink != 0 {
			return root.Lchown(name, -1, gid)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		mode := info.Mode()
		if d.IsDir() {
			mode |= os.ModeSetgid | 0o070
		} else {
			mode |= 0o060
		}

		if err = root.Chmod(name, mode); err != nil {
			return err
		}

		return root.Lchown(name, -1, gid)
	})
}
