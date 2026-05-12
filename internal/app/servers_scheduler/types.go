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
