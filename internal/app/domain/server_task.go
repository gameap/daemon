package domain

import (
	"context"
	"encoding/json"
	"time"
)

type ServerTaskStatus int

const (
	ServerTaskStatusWaiting ServerTaskStatus = iota
	ServerTaskStatusWorking
	ServerTaskStatusSuccess
	ServerTaskStatusFail
)

type ServerTaskCommand string

const (
	ServerTaskStart     ServerTaskCommand = "start"
	ServerTaskStop      ServerTaskCommand = "stop"
	ServerTaskRestart   ServerTaskCommand = "restart"
	ServerTaskUpdate    ServerTaskCommand = "update"
	ServerTaskReinstall ServerTaskCommand = "reinstall"
)

type ServerTaskRepository interface {
	Find(ctx context.Context) ([]*ServerTask, error)
	Save(ctx context.Context, task *ServerTask) error
	Fail(ctx context.Context, task *ServerTask, output []byte) error
}

type ServerTask struct {
	ID           int
	Status       ServerTaskStatus
	Command      ServerTaskCommand
	Server       *Server
	Repeat       int
	RepeatPeriod time.Duration
	Counter      int
	ExecuteDate  time.Time
}

func NewServerTask(
	id int,
	command ServerTaskCommand,
	server *Server,
	repeat int,
	repeatPeriod time.Duration,
	counter int,
	executeDate time.Time,
) *ServerTask {
	return &ServerTask{
		id,
		ServerTaskStatusWaiting,
		command,
		server,
		repeat,
		repeatPeriod,
		counter,
		executeDate,
	}
}

func (s ServerTask) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Repeat                int    `json:"repeat"`
		RepeatPeriodInSeconds int    `json:"repeat_period"`
		ExecuteDate           string `json:"execute_date"`
	}{
		Repeat: s.Repeat,
		RepeatPeriodInSeconds: int(s.RepeatPeriod.Seconds()),
		ExecuteDate: s.ExecuteDate.Format("2006-01-02 15:04:05"),
	})
}
