//go:build linux
// +build linux

package components

import (
	"os/exec"
	"strconv"
	"syscall"

	"github.com/pkg/errors"
)

func setCMDSysProcCredential(cmd *exec.Cmd, options ExecutorOptions) (*exec.Cmd, error) {
	uid, err := strconv.Atoi(options.UID)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	gid, err := strconv.Atoi(options.UID)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}

	return cmd, nil
}
