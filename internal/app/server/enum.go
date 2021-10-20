package server

type Mode int

const (
	ModeNoAuth Mode = iota
	ModeAuth
	ModeCommands
	ModeFiles
	ModeStatus

	ModeUnknown = -1
)
