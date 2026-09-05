package gdaemonscheduler

import "errors"

var (
	ErrInvalidTaskError = errors.New("invalid task")
	ErrServerBusy       = errors.New("another command is already running for this server")
)
