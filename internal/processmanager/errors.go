package processmanager

import "github.com/pkg/errors"

var ErrUnknownProcessManager = errors.New("unknown process manager")
var ErrEmptyUser = errors.New("empty user")
var ErrUserNotFound = errors.New("user not found")
var ErrInvalidUserPassword = errors.New("invalid user password")
