package serversscheduler

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/gameap/daemon/pkg/logger"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Scheduler) executeNow(parent context.Context, rec *executionRecord, server *domain.Server) {
	ctx, cancel := context.WithCancel(parent)

	s.mu.Lock()
	rec.cancel = cancel
	s.mu.Unlock()

	defer cancel()
	defer s.completeAndAdvance(parent, rec)

	s.sendStarted(rec)

	domainCmd, ok := mapProtoCommandToDomain(rec.command)
	if !ok {
		s.sendFinished(rec,
			pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_FAILED,
			"unknown server task command", nil, s.now())
		return
	}

	cmd := s.commandLoader.LoadServerCommand(domainCmd, server)
	if cmd == nil {
		s.sendFinished(rec,
			pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_FAILED,
			"no server command implementation", nil, s.now())
		return
	}

	err := cmd.Execute(ctx, server)
	output := cmd.ReadOutput()
	finishedAt := s.now()

	status := pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS
	var errMsg string

	switch {
	case ctx.Err() != nil && parent.Err() == nil:
		status = pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED
	case err != nil:
		status = pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_FAILED
		errMsg = truncateError(err.Error())
		logger.Logger(parent).WithError(err).WithField("task_id", rec.taskID).
			Warn("Server task command failed")
	case cmd.Result() == gameservercommands.ErrorResult:
		status = pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_FAILED
	}

	if status == pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS {
		server.NoticeTaskCompleted()
	}

	s.sendFinished(rec, status, errMsg, output, finishedAt)
}

func (s *Scheduler) sendStarted(rec *executionRecord) {
	s.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_ServerTaskExecutionStarted{
			ServerTaskExecutionStarted: &pb.ServerTaskExecutionStarted{
				ExecutionId: rec.execID,
				TaskId:      rec.taskID,
				TaskVersion: rec.taskVersion,
				ServerId:    rec.serverID,
				NodeId:      rec.nodeID,
				Command:     rec.command,
				StartedAt:   timestamppb.New(rec.startedAt),
			},
		},
	})
}

func (s *Scheduler) sendFinishedSkipped(rec *executionRecord) {
	now := s.now()
	s.sendFinished(rec,
		pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SKIPPED,
		"overlap policy SKIP: previous execution still running", nil, now)
}

func (s *Scheduler) sendFinished(
	rec *executionRecord,
	status pb.ServerTaskExecutionStatus,
	errMsg string,
	output []byte,
	finishedAt time.Time,
) {
	chunks, inlineTail, streamed := splitOutput(output)

	if streamed {
		for i, chunk := range chunks {
			s.sender.Send(&pb.DaemonMessage{
				Payload: &pb.DaemonMessage_ServerTaskExecutionLog{
					ServerTaskExecutionLog: &pb.ServerTaskExecutionLog{
						ExecutionId: rec.execID,
						Sequence:    uint64(i + 1),
						Chunk:       chunk,
						IsFinal:     i == len(chunks)-1,
					},
				},
			})
		}
	}

	s.sender.Send(&pb.DaemonMessage{
		Payload: &pb.DaemonMessage_ServerTaskExecutionFinished{
			ServerTaskExecutionFinished: &pb.ServerTaskExecutionFinished{
				ExecutionId:       rec.execID,
				TaskId:            rec.taskID,
				Status:            status,
				ExitCode:          0,
				ErrorMessage:      errMsg,
				FinishedAt:        timestamppb.New(finishedAt),
				Duration:          durationpb.New(finishedAt.Sub(rec.startedAt)),
				OutputInline:      inlineTail,
				OutputStreamed:    streamed,
				OutputStoragePath: "",
			},
		},
	})

	log.WithFields(log.Fields{
		"execution_id": rec.execID,
		"task_id":      rec.taskID,
		"status":       status.String(),
	}).Debug("Server task execution finished")
}
