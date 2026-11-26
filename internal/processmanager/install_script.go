//go:build linux || darwin || windows

package processmanager

import (
	"io/fs"
	"os"
	"strconv"

	"github.com/pkg/errors"
)

const (
	// installScriptName is the file the installation script is handed to the
	// installation container in: it lies in the server directory, which the
	// container has mounted.
	installScriptName = ".gameap_install.sh"

	installScriptPerm os.FileMode = 0o755
	serverDirPerm     os.FileMode = 0o755
)

// writeInstallScript puts the installation script into the server directory.
//
// The directory belongs to the tenant of the server and the script is written
// with the privileges of the daemon, so the file is created anew through the
// root the directory was opened with. Whatever stands under its name goes
// first: a symlink left there is taken away, not written through, and the mode
// is set on the file that was created, not on what its name leads to.
func writeInstallScript(serverDir *os.Root, script string) error {
	err := serverDir.RemoveAll(installScriptName)
	if err != nil {
		return errors.Wrapf(err, "failed to remove what stands in place of %s", installScriptName)
	}

	file, err := serverDir.OpenFile(installScriptName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, installScriptPerm)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s", installScriptName)
	}

	_, writeErr := file.WriteString(normalizeLineEndings(script))
	chmodErr := file.Chmod(installScriptPerm)
	closeErr := file.Close()

	switch {
	case writeErr != nil:
		err = errors.Wrapf(writeErr, "failed to write %s", installScriptName)
	case chmodErr != nil:
		err = errors.Wrapf(chmodErr, "failed to make %s executable", installScriptName)
	case closeErr != nil:
		err = errors.Wrapf(closeErr, "failed to close %s", installScriptName)
	}

	if err != nil {
		removeInstallScript(serverDir)
	}

	return err
}

func removeInstallScript(serverDir *os.Root) {
	_ = serverDir.Remove(installScriptName)
}

// chownRecursive changes ownership of the directory serverDir is opened at and
// of all its contents. A symlink is given the owner itself, what it leads to is
// left alone.
func chownRecursive(serverDir *os.Root, uidStr, gidStr string) error {
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return errors.Wrapf(err, "invalid uid: %s", uidStr)
	}
	gid, err := strconv.Atoi(gidStr)
	if err != nil {
		return errors.Wrapf(err, "invalid gid: %s", gidStr)
	}

	return fs.WalkDir(serverDir.FS(), ".", func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		return serverDir.Lchown(p, uid, gid)
	})
}
