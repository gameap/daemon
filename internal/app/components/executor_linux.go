//go:build linux
// +build linux

package components

import (
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

func setCMDSysProcAttr(cmd *exec.Cmd, options domain.ExecutorOptions) (*exec.Cmd, error) {
	var uid, gid int
	var err error

	if options.UID != "" {
		uid, err = strconv.Atoi(options.UID)
		if err != nil {
			return nil, errors.WithMessage(err, "[components.executor] invalid user uid")
		}
	}

	if options.GID != "" {
		gid, err = strconv.Atoi(options.GID)
		if err != nil {
			return nil, errors.WithMessage(err, "[components.executor] invalid user gid")
		}
	}

	if uid == 0 && gid == 0 && options.Username != "" {
		uid, gid, err = findUIDAndGIDByUsername(options.Username)
		if err != nil {
			return nil, err
		}
	}

	if uid != 0 && gid != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	return cmd, nil
}

func findUIDAndGIDByUsername(username string) (int, int, error) {
	systemUser, err := user.Lookup(username)
	if err != nil {
		return 0, 0, errors.WithMessage(err, "[components] failed to lookup user")
	}

	uid, err := strconv.Atoi(systemUser.Uid)
	if err != nil {
		return 0, 0, errors.WithMessage(err, "[components] invalid user uid")
	}
	gid, err := strconv.Atoi(systemUser.Uid)
	if err != nil {
		return 0, 0, errors.WithMessage(err, "[components] invalid user gid")
	}

	return uid, gid, nil
}
