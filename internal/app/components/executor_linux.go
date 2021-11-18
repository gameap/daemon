//go:build windows
// +build windows

package components

func setCMDSysProcCredential(cmd *exec.Cmd, options ExecutorOptions) *exec.Cmd {
	uid, err := strconv.Atoi(options.UID)
	if err != nil {
		return -1, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
	}

	gid, err := strconv.Atoi(options.UID)
	if err != nil {
		return -1, errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}

	return cmd
}
