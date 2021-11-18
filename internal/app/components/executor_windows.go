//go:build windows
// +build windows

package components

import "os/exec"

func setCMDSysProcCredential(cmd *exec.Cmd, _ ExecutorOptions) (*exec.Cmd, error) {
	return cmd, nil
}
