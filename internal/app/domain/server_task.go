package domain

import (
	"sync"
	"time"
)

type ServerTaskCommand string

const (
	ServerTaskStart     ServerTaskCommand = "start"
	ServerTaskStop      ServerTaskCommand = "stop"
	ServerTaskRestart   ServerTaskCommand = "restart"
	ServerTaskUpdate    ServerTaskCommand = "update"
	ServerTaskReinstall ServerTaskCommand = "reinstall"
)

type ServerTaskOverlapPolicy int

const (
	ServerTaskOverlapUnspecified ServerTaskOverlapPolicy = iota
	ServerTaskOverlapSkip
	ServerTaskOverlapQueue
)

type ServerTaskCatchupPolicy int

const (
	ServerTaskCatchupUnspecified ServerTaskCatchupPolicy = iota
	ServerTaskCatchupSkip
	ServerTaskCatchupRunOnce
)

type ServerTask struct {
	mutex *sync.Mutex

	id       uint64
	serverID uint64
	nodeID   uint64
	version  uint64
	command  ServerTaskCommand
	server   *Server

	executeDate  time.Time
	repeat       int
	repeatPeriod time.Duration
	counter      int

	overlapPolicy ServerTaskOverlapPolicy
	catchupPolicy ServerTaskCatchupPolicy

	name      string
	timezone  string
	payload   string
	enabled   bool
	updatedAt time.Time
}

type ServerTaskOptions struct {
	ID            uint64
	ServerID      uint64
	NodeID        uint64
	Version       uint64
	Command       ServerTaskCommand
	Server        *Server
	ExecuteDate   time.Time
	Repeat        int
	RepeatPeriod  time.Duration
	Counter       int
	OverlapPolicy ServerTaskOverlapPolicy
	CatchupPolicy ServerTaskCatchupPolicy
	Name          string
	Timezone      string
	Payload       string
	Enabled       bool
	UpdatedAt     time.Time
}

func NewServerTask(opts ServerTaskOptions) *ServerTask {
	return &ServerTask{
		mutex:         &sync.Mutex{},
		id:            opts.ID,
		serverID:      opts.ServerID,
		nodeID:        opts.NodeID,
		version:       opts.Version,
		command:       opts.Command,
		server:        opts.Server,
		executeDate:   opts.ExecuteDate,
		repeat:        opts.Repeat,
		repeatPeriod:  opts.RepeatPeriod,
		counter:       opts.Counter,
		overlapPolicy: opts.OverlapPolicy,
		catchupPolicy: opts.CatchupPolicy,
		name:          opts.Name,
		timezone:      opts.Timezone,
		payload:       opts.Payload,
		enabled:       opts.Enabled,
		updatedAt:     opts.UpdatedAt,
	}
}

func (s *ServerTask) ID() uint64 {
	return s.id
}

func (s *ServerTask) ServerID() uint64 {
	return s.serverID
}

func (s *ServerTask) NodeID() uint64 {
	return s.nodeID
}

func (s *ServerTask) Version() uint64 {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.version
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

func (s *ServerTask) SetExecuteDate(t time.Time) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.executeDate = t
}

func (s *ServerTask) Counter() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.counter
}

func (s *ServerTask) OverlapPolicy() ServerTaskOverlapPolicy {
	return s.overlapPolicy
}

func (s *ServerTask) CatchupPolicy() ServerTaskCatchupPolicy {
	return s.catchupPolicy
}

func (s *ServerTask) Name() string {
	return s.name
}

func (s *ServerTask) Timezone() string {
	return s.timezone
}

func (s *ServerTask) Payload() string {
	return s.payload
}

func (s *ServerTask) Enabled() bool {
	return s.enabled
}

func (s *ServerTask) UpdatedAt() time.Time {
	return s.updatedAt
}

func (s *ServerTask) IncreaseCountersAndTime() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.prolongTask()
	s.counter++
}

func (s *ServerTask) ProlongTime() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.prolongTask()
}

func (s *ServerTask) RepeatEndlessly() bool {
	return s.repeat == 0 || s.repeat == -1
}

func (s *ServerTask) CanExecute() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.RepeatEndlessly() || s.repeat > s.counter
}

func (s *ServerTask) IsActive() bool {
	return s.enabled && s.CanExecute()
}

// UpdateFromOptions atomically applies new fields received via gRPC delta.
// Used by the scheduler when API pushes ServerTaskDelta.upserted; preserves
// counter and mutex state.
func (s *ServerTask) UpdateFromOptions(opts ServerTaskOptions) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.serverID = opts.ServerID
	s.nodeID = opts.NodeID
	s.version = opts.Version
	s.command = opts.Command
	if opts.Server != nil {
		s.server = opts.Server
	}
	s.executeDate = opts.ExecuteDate
	s.repeat = opts.Repeat
	s.repeatPeriod = opts.RepeatPeriod
	s.counter = opts.Counter
	s.overlapPolicy = opts.OverlapPolicy
	s.catchupPolicy = opts.CatchupPolicy
	s.name = opts.Name
	s.timezone = opts.Timezone
	s.payload = opts.Payload
	s.enabled = opts.Enabled
	s.updatedAt = opts.UpdatedAt
}

func (s *ServerTask) prolongTask() {
	s.executeDate = s.executeDate.Add(s.repeatPeriod)
}
