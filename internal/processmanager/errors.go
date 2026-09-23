package processmanager

import "github.com/pkg/errors"

var (
	ErrUnknownProcessManager = errors.New("unknown process manager")
	ErrEmptyUser             = errors.New("empty user")
	ErrUserNotFound          = errors.New("user not found")
	ErrInvalidUserPassword   = errors.New("invalid user password")
	ErrEmptyCommand          = errors.New("empty command")
	ErrNotImplemented        = errors.New("not implemented")
	ErrContainerNotRunning   = errors.New("container is not running")
	ErrServiceNotRunning     = errors.New("service is not running")
	ErrServiceNotFound       = errors.New("service is not registered")
	ErrServiceStateTimeout   = errors.New("timeout waiting for the service to change state")
	ErrServiceStoppedOnStart = errors.New("service stopped immediately after start")
	ErrSocketStopFailed      = errors.New("failed to stop socket unit")
	ErrUserMismatch          = errors.New(
		"server user does not match daemon user (required for systemctl --user mode)",
	)

	ErrProcessSnapshotMalformed = errors.New("malformed process list")
	ErrProcessSnapshotTooLarge  = errors.New("process list does not fit the buffer")
)
