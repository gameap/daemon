package domain

import (
	"context"
	"encoding/json"
	"sync"
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
	executeDate  time.Time
	server       *Server
	mutex        *sync.Mutex
	command      ServerTaskCommand
	id           int
	status       ServerTaskStatus
	repeat       int
	repeatPeriod time.Duration
	counter      int
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
		id:           id,
		status:       ServerTaskStatusWaiting,
		command:      command,
		server:       server,
		repeat:       repeat,
		repeatPeriod: repeatPeriod,
		counter:      counter,
		executeDate:  executeDate,
		mutex:        &sync.Mutex{},
	}
}

func (s ServerTask) MarshalJSON() ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return json.Marshal(struct {
		ExecuteDate           string `json:"execute_date"`
		Repeat                int    `json:"repeat"`
		RepeatPeriodInSeconds int    `json:"repeat_period"`
	}{
		ExecuteDate:           s.executeDate.Format("2006-01-02 15:04:05"),
		Repeat:                s.repeat,
		RepeatPeriodInSeconds: int(s.repeatPeriod.Seconds()),
	})
}

func (s *ServerTask) ID() int {
	return s.id
}

func (s *ServerTask) Status() ServerTaskStatus {
	return s.status
}

func (s *ServerTask) Command() ServerTaskCommand {
	return s.command
}

func (s *ServerTask) Server() *Server {
	return s.server
}

func (s *ServerTask) Repeat() int {
	return s.repeat
}

func (s *ServerTask) RepeatPeriod() time.Duration {
	return s.repeatPeriod
}

func (s *ServerTask) ExecuteDate() time.Time {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.executeDate
}

func (s *ServerTask) Counter() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.counter
}

func (s *ServerTask) IncreaseCountersAndTime() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.prolongTask()
	s.counter++
}

func (s *ServerTask) RepeatEndlessly() bool {
	return s.repeat == 0 || s.repeat == -1
}

func (s *ServerTask) CanExecute() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.RepeatEndlessly() || s.repeat > s.counter
}

func (s *ServerTask) prolongTask() {
	s.executeDate = s.executeDate.Add(s.repeatPeriod)

	if s.executeDate.Before(time.Now()) {
		s.executeDate = time.Now().Add(s.repeatPeriod)
	}
}
