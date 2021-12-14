//go:build windows
// +build windows

package components

import (
	"os/exec"

	"github.com/gameap/daemon/internal/app/contracts"
)

func setCMDSysProcCredential(cmd *exec.Cmd, _ contracts.ExecutorOptions) (*exec.Cmd, error) {
	return cmd, nil
}
