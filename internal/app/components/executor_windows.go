//go:build windows
// +build windows

package components

import (
	"github.com/gameap/daemon/internal/app/contracts"
	"os/exec"
)

func setCMDSysProcCredential(cmd *exec.Cmd, _ contracts.ExecutorOptions) (*exec.Cmd, error) {
	return cmd, nil
}

//nolint:lll
//func ExecWithWriter(ctx context.Context, command string, out io.Writer, options contracts.ExecutorOptions) (int, error) {
//	if command == "" {
//		return invalidResult, ErrEmptyCommand
//	}
//
//	args, err := shellquote.Split(command)
//	if err != nil {
//		return invalidResult, err
//	}
//
//	workDir := options.WorkDir
//	_, err = os.Stat(workDir)
//	if err != nil && options.FallbackWorkDir == "" {
//		return invalidResult, errors.Wrapf(err, "invalid work directory %s", workDir)
//	} else if err != nil && options.FallbackWorkDir != "" {
//		_, err = os.Stat(options.FallbackWorkDir)
//		if err != nil {
//			return invalidResult, errors.Wrapf(err, "invalid fallback work directory %s", options.FallbackWorkDir)
//		}
//
//		workDir = options.FallbackWorkDir
//	}
//
//	cmdArgs := []string{"/c"}
//	cmdArgs = append(cmdArgs, args...)
//
//	cmd := exec.CommandContext(ctx, "cmd", cmdArgs...) //nolint:gosec
//	cmd.Dir = workDir
//	cmd.Stdout = out
//	cmd.Stderr = out
//
//	if options.UID != "" && options.GID != "" {
//		cmd, err = setCMDSysProcCredential(cmd, options)
//		if err != nil {
//			return invalidResult, err
//		}
//	}
//
//	err = cmd.Run()
//	if err != nil {
//		_, ok := err.(*exec.ExitError)
//		if !ok {
//			log.Warning(err)
//
//			return invalidResult, err
//		}
//	}
//
//	return cmd.ProcessState.ExitCode(), nil
//}
