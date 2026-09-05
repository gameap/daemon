package processmanager

import (
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

// ErrStatusUndetermined is returned when a liveness probe could not be
// evaluated. It is deliberately distinct from "the server is not running": the
// servers loop restarts a server it believes to be down, so a probe that was
// cut short must never be reported as a stopped server.
var ErrStatusUndetermined = errors.New("server status could not be determined")

// statusFromExitCode maps the exit code of a liveness probe to a result.
//
// Zero means running and a positive code means stopped, which is the convention
// every status script and `tmux has-session` follows. A negative code is not an
// exit code at all: os/exec reports it for a process that died on a signal,
// including the SIGKILL that exec.CommandContext delivers when the probe's
// deadline passes. That case carries no information about the game server.
func statusFromExitCode(code int) (domain.Result, error) {
	switch {
	case code == 0:
		return domain.SuccessResult, nil
	case code > 0:
		return domain.ErrorResult, nil
	default:
		return domain.UnknownResult, errors.WithMessagef(
			ErrStatusUndetermined, "status command terminated abnormally with code %d", code,
		)
	}
}
