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
		Repeat:                s.Repeat,
		RepeatPeriodInSeconds: int(s.RepeatPeriod.Seconds()),
		ExecuteDate:           s.ExecuteDate.Format("2006-01-02 15:04:05"),
	})
}

func (s *ServerTask) IncreaseCountersAndTime() {
	s.prolongTask()
	s.Counter++
}

func (s *ServerTask) RepeatEndlessly() bool {
	return s.Repeat == 0 || s.Repeat == -1
}

func (s *ServerTask) CanExecute() bool {
	return s.RepeatEndlessly() || s.Repeat > s.Counter
}

func (s *ServerTask) prolongTask() {
	s.ExecuteDate = s.ExecuteDate.Add(s.RepeatPeriod)

	if s.ExecuteDate.Before(time.Now()) {
		s.ExecuteDate = time.Now().Add(s.RepeatPeriod)
	}
}
