package domain

import "time"

type ServerTaskStatus int

const (
	ServerTaskStatusWaiting = iota
	ServerTaskStatusWorking
	ServerTaskStatusSuccess
	ServerTaskStatusFail
)

type ServerTask struct {
	Status       ServerTaskStatus
	ID           int
	Command      int
	ServerID     int
	Repeat       int
	RepeatPeriod int
	Counter      int
	ExecuteDate  time.Time
	Payload      string
}
