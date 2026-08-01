package serversscheduler

import (
	"context"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancel_RunningExecution_EmitsCanceled(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	block := make(chan struct{})

	loader := &fakeLoader{cmd: &fakeCommand{block: block}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:           20,
		ServerID:     42,
		Version:      1,
		Command:      domain.ServerTaskRestart,
		ExecuteDate:  now.Add(-time.Second),
		RepeatPeriod: time.Hour,
		Enabled:      true,
		Server:       server,
	})
	scheduler.cache.Put(task)

	scheduler.tick(context.Background())
	waitForStarted(t, sender, 1)

	execID := sender.Started()[0].ExecutionId
	scheduler.CancelExecution(&pb.ServerTaskExecutionCancel{
		ExecutionId: execID,
		TaskId:      20,
		Reason:      "user requested",
	})

	waitForFinished(t, sender, 1)
	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, execID, finished[0].ExecutionId)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED, finished[0].Status)

	// channel was never closed; the goroutine returns via ctx cancel.
	_ = block
}

func TestCancel_QueuedExecution_EmitsStartedAndCanceled(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	block := make(chan struct{})
	defer close(block)

	loader := &fakeLoader{cmd: &fakeCommand{block: block}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            21,
		ServerID:      42,
		Version:       1,
		Command:       domain.ServerTaskRestart,
		ExecuteDate:   now.Add(-time.Second),
		RepeatPeriod:  time.Hour,
		OverlapPolicy: domain.ServerTaskOverlapQueue,
		Enabled:       true,
		Server:        server,
	})
	scheduler.cache.Put(task)

	scheduler.tick(context.Background())
	waitForStarted(t, sender, 1)

	task.SetExecuteDate(now.Add(-time.Second))
	scheduler.tick(context.Background())

	scheduler.mu.Lock()
	rt := scheduler.inFlight[21]
	require.NotNil(t, rt)
	require.Len(t, rt.queued, 1, "second fire must be queued")
	queuedExecID := rt.queued[0].execID
	scheduler.mu.Unlock()

	scheduler.CancelExecution(&pb.ServerTaskExecutionCancel{
		ExecutionId: queuedExecID,
		TaskId:      21,
		Reason:      "user cancel queued",
	})

	// Both events for the queued execution: Started + Finished(CANCELED).
	deadline := time.Now().Add(2 * time.Second)
	var canceled *pb.ServerTaskExecutionFinished
	for time.Now().Before(deadline) && canceled == nil {
		for _, f := range sender.Finished() {
			if f.ExecutionId == queuedExecID &&
				f.Status == pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED {
				canceled = f
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	require.NotNil(t, canceled, "queued cancel must emit Finished(CANCELED)")

	var queuedStarted bool
	for _, s := range sender.Started() {
		if s.ExecutionId == queuedExecID {
			queuedStarted = true
			break
		}
	}
	assert.True(t, queuedStarted, "queued cancel must also emit Started so api row exists")
}

func TestCancel_UnknownExecID_EmitsSafetyNetCanceled(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	scheduler.CancelExecution(&pb.ServerTaskExecutionCancel{
		ExecutionId: "unknown-id",
		TaskId:      42,
		Reason:      "stale",
	})

	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, "unknown-id", finished[0].ExecutionId)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_CANCELED, finished[0].Status)
}
