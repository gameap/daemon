//go:build linux || darwin
// +build linux darwin

package components

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/pkg/errors"
)

func setCMDSysProcCredential(cmd *exec.Cmd, options contracts.ExecutorOptions) (*exec.Cmd, error) {
	u, err := user.LookupId(options.UID)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), NoSetGroups: true}

	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, "HOME="+u.HomeDir)

	return cmd, nil
}
