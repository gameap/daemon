package processmanager

import "github.com/pkg/errors"

var (
	ErrUnknownProcessManager = errors.New("unknown process manager")
	ErrEmptyUser             = errors.New("empty user")
	ErrUserNotFound          = errors.New("user not found")
	ErrInvalidUserPassword   = errors.New("invalid user password")
	ErrEmptyCommand          = errors.New("empty command")
)
