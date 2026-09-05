package serversscheduler

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
)

type ServerTaskSender interface {
	Send(msg *pb.DaemonMessage)
}

type CommandLoader interface {
	LoadServerCommand(cmd domain.ServerCommand, server *domain.Server) contracts.GameServerCommand
}

// serverLocker is implemented by the shared command factory. The scheduler keys
// its own in-flight map by task, so two tasks for one server, or a scheduled
// task and an automatic start from the servers loop, would otherwise run at the
// same time.
type serverLocker interface {
	TryLockServer(serverID int) (func(), bool)
}

type executionRecord struct {
	execID      string
	taskID      uint64
	taskVersion uint64
	serverID    uint64
	nodeID      uint64
	command     pb.ServerTaskCommand
	payload     string
	startedAt   time.Time
	cancel      context.CancelFunc
}

type runningTask struct {
	current *executionRecord
	queued  []*executionRecord
}
