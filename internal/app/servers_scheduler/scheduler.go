package serversscheduler

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const tickInterval = 5 * time.Second

type Scheduler struct {
	cfg           *config.Config
	commandLoader CommandLoader
	serverRepo    domain.ServerRepository
	sender        ServerTaskSender

	cache  *taskCache
	resync *resyncTrigger

	mu       sync.Mutex
	inFlight map[uint64]*runningTask
	byExecID map[string]*executionRecord

	nowFn func() time.Time
}

func NewScheduler(
	cfg *config.Config,
	commandLoader CommandLoader,
	serverRepo domain.ServerRepository,
	sender ServerTaskSender,
) *Scheduler {
	s := &Scheduler{
		cfg:           cfg,
		commandLoader: commandLoader,
		serverRepo:    serverRepo,
		sender:        sender,
		cache:         newTaskCache(),
		inFlight:      make(map[uint64]*runningTask),
		byExecID:      make(map[string]*executionRecord),
		nowFn:         time.Now,
	}
	s.resync = newResyncTrigger(sender, s.now)
	return s
}

func (s *Scheduler) now() time.Time {
	return s.nowFn()
}

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	now := s.now()

	for _, task := range s.cache.Snapshot() {
		if ctx.Err() != nil {
			return
		}
		if !task.IsActive() {
			continue
		}
		if task.ExecuteDate().After(now) {
			continue
		}

		server := task.Server()
		if server == nil {
			resolved, err := s.serverRepo.FindByID(ctx, int(task.ServerID()))
			if err != nil || resolved == nil {
				logger.Logger(ctx).WithError(err).WithFields(log.Fields{
					"task_id":   task.ID(),
					"server_id": task.ServerID(),
				}).Warn("Skipping server task: server not in local cache")
				continue
			}
			server = resolved
		}

		s.fire(ctx, task, server)
	}
}

func (s *Scheduler) fire(ctx context.Context, task *domain.ServerTask, server *domain.Server) {
	s.mu.Lock()

	rt, ok := s.inFlight[task.ID()]
	if !ok {
		rt = &runningTask{}
		s.inFlight[task.ID()] = rt
	}

	rec := &executionRecord{
		execID:      uuid.NewString(),
		taskID:      task.ID(),
		taskVersion: task.Version(),
		serverID:    task.ServerID(),
		nodeID:      task.NodeID(),
		command:     domainCommandToProto(task.Command()),
		payload:     task.Payload(),
		startedAt:   s.now(),
	}

	if rt.current != nil {
		switch task.OverlapPolicy() {
		case domain.ServerTaskOverlapQueue:
			rt.queued = append(rt.queued, rec)
			s.byExecID[rec.execID] = rec
			s.mu.Unlock()
			task.IncreaseCountersAndTime()
			return
		default:
			// SKIP (and unspecified) emits Started+Finished(SKIPPED) immediately.
			s.byExecID[rec.execID] = rec
			s.mu.Unlock()
			task.IncreaseCountersAndTime()
			s.sendStarted(rec)
			s.sendFinishedSkipped(rec)
			s.cleanupFinished(rec.execID)
			return
		}
	}

	rt.current = rec
	s.byExecID[rec.execID] = rec
	s.mu.Unlock()
	task.IncreaseCountersAndTime()

	go s.executeNow(ctx, rec, server)
}

func (s *Scheduler) ApplySnapshot(snap *pb.ServerTaskSnapshot) {
	if snap == nil {
		return
	}
	now := s.now()

	tasks := make([]*domain.ServerTask, 0, len(snap.GetTasks()))
	for _, pt := range snap.GetTasks() {
		opts := protoToTaskOptions(pt, s.resolveServer(pt.GetServerId()))
		var t *domain.ServerTask
		if cached := s.cache.Get(opts.ID); cached != nil {
			cached.UpdateFromOptions(opts)
			t = cached
		} else {
			t = domain.NewServerTask(opts)
		}
		applyCatchupOnApply(t, now)
		tasks = append(tasks, t)
	}

	s.cache.Replace(tasks)

	log.WithFields(log.Fields{
		"count":            len(tasks),
		"snapshot_version": snap.GetSnapshotVersion(),
	}).Info("Server task snapshot applied")
}

func (s *Scheduler) ApplyDelta(delta *pb.ServerTaskDelta) {
	if delta == nil {
		return
	}

	if upserted := delta.GetUpserted(); upserted != nil {
		s.applyUpsert(upserted)
		return
	}

	if deleted := delta.GetDeleted(); deleted != nil {
		s.applyDeleted(deleted)
	}
}

func (s *Scheduler) applyUpsert(pt *pb.ServerTask) {
	cached := s.cache.Get(pt.GetId())
	switch {
	case cached == nil && pt.GetVersion() > 1:
		log.WithFields(log.Fields{
			"task_id": pt.GetId(),
			"version": pt.GetVersion(),
		}).Info("Unknown task with version > 1: requesting resync")
		s.resync.Trigger()
	case cached != nil && pt.GetVersion() <= cached.Version():
		log.WithFields(log.Fields{
			"task_id":        pt.GetId(),
			"delta_version":  pt.GetVersion(),
			"cached_version": cached.Version(),
		}).Debug("Stale server task delta dropped")
		return
	case cached != nil && pt.GetVersion() > cached.Version()+1:
		log.WithFields(log.Fields{
			"task_id":        pt.GetId(),
			"delta_version":  pt.GetVersion(),
			"cached_version": cached.Version(),
		}).Info("Server task version gap: requesting resync")
		s.resync.Trigger()
	}

	opts := protoToTaskOptions(pt, s.resolveServer(pt.GetServerId()))
	var t *domain.ServerTask
	if cached != nil {
		cached.UpdateFromOptions(opts)
		t = cached
	} else {
		t = domain.NewServerTask(opts)
	}
	applyCatchupOnApply(t, s.now())
	s.cache.Put(t)
}

func (s *Scheduler) applyDeleted(deleted *pb.ServerTaskDeleted) {
	cached := s.cache.Get(deleted.GetId())
	if cached == nil {
		return
	}
	if deleted.GetVersion() <= cached.Version() {
		return
	}
	s.cache.Delete(deleted.GetId())
}

func (s *Scheduler) CancelExecution(req *pb.ServerTaskExecutionCancel) {
	if req == nil {
		return
	}

	s.mu.Lock()
	rec, ok := s.byExecID[req.GetExecutionId()]
	if !ok {
		s.mu.Unlock()
		s.sendStarted(&executionRecord{
			execID:    req.GetExecutionId(),
			taskID:    req.GetTaskId(),
			startedAt: s.now(),
		})
		s.sendFinished(&executionRecord{
			execID:    req.GetExecutionId(),
			taskID:    req.GetTaskId(),
			startedAt: s.now(),
		}, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED, req.GetReason(), nil, s.now())
		return
	}

	rt := s.inFlight[rec.taskID]
	if rt != nil && rt.current == rec {
		s.mu.Unlock()
		rec.cancel()
		return
	}

	if rt != nil {
		for i, q := range rt.queued {
			if q == rec {
				rt.queued = append(rt.queued[:i], rt.queued[i+1:]...)
				delete(s.byExecID, rec.execID)
				s.mu.Unlock()
				s.sendStarted(rec)
				s.sendFinished(rec,
					pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED,
					req.GetReason(), nil, s.now())
				return
			}
		}
	}

	s.mu.Unlock()
}

func (s *Scheduler) AckExecution(ack *pb.ServerTaskExecutionAck) {
	if ack == nil {
		return
	}
	log.WithField("execution_id", ack.GetExecutionId()).Debug("Server task execution ack received")
}

func (s *Scheduler) InFlightExecutions() []*pb.InFlightServerTaskExecution {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*pb.InFlightServerTaskExecution, 0)
	for _, rt := range s.inFlight {
		if rt.current == nil {
			continue
		}
		out = append(out, &pb.InFlightServerTaskExecution{
			ExecutionId: rt.current.execID,
			TaskId:      rt.current.taskID,
			StartedAt:   timestamppb.New(rt.current.startedAt),
		})
	}
	return out
}

func (s *Scheduler) resolveServer(serverID uint64) *domain.Server {
	if serverID == 0 {
		return nil
	}
	server, err := s.serverRepo.FindByID(context.Background(), int(serverID))
	if err != nil {
		return nil
	}
	return server
}

func (s *Scheduler) cleanupFinished(execID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.byExecID, execID)
}

func (s *Scheduler) completeAndAdvance(ctx context.Context, rec *executionRecord) {
	s.mu.Lock()

	rt := s.inFlight[rec.taskID]
	delete(s.byExecID, rec.execID)
	if rt == nil {
		s.mu.Unlock()
		return
	}

	rt.current = nil
	if len(rt.queued) == 0 {
		s.mu.Unlock()
		return
	}

	next := rt.queued[0]
	rt.queued = rt.queued[1:]
	rt.current = next
	s.mu.Unlock()

	server := s.resolveServer(next.serverID)
	if server == nil {
		s.sendStarted(next)
		s.sendFinished(next,
			pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_FAILED,
			"server not available", nil, s.now())
		s.completeAndAdvance(ctx, next)
		return
	}

	go s.executeNow(ctx, next, server)
}

func domainCommandToProto(c domain.ServerTaskCommand) pb.ServerTaskCommand {
	switch c {
	case domain.ServerTaskStart:
		return pb.ServerTaskCommand_SERVER_TASK_COMMAND_START
	case domain.ServerTaskStop:
		return pb.ServerTaskCommand_SERVER_TASK_COMMAND_STOP
	case domain.ServerTaskRestart:
		return pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART
	case domain.ServerTaskUpdate:
		return pb.ServerTaskCommand_SERVER_TASK_COMMAND_UPDATE
	case domain.ServerTaskReinstall:
		return pb.ServerTaskCommand_SERVER_TASK_COMMAND_REINSTALL
	}
	return pb.ServerTaskCommand_SERVER_TASK_COMMAND_UNSPECIFIED
}
