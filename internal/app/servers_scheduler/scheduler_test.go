package serversscheduler

import (
	"context"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestTick_FiresDueTask_EmitsStartedAndFinishedSuccess(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	loader := &fakeLoader{cmd: &fakeCommand{output: []byte("ok")}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	snap := &pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            7,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(now.Add(-time.Second)),
			RepeatPeriod:  durationpb.New(time.Hour),
			RepeatCount:   0,
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
			Enabled:       true,
		}},
	}
	scheduler.ApplySnapshot(snap)

	scheduler.tick(context.Background())
	waitForFinished(t, sender, 1)

	started := sender.Started()
	require.Len(t, started, 1)
	assert.Equal(t, uint64(7), started[0].TaskId)
	assert.Equal(t, uint64(42), started[0].ServerId)
	assert.Equal(t, uint64(1), started[0].TaskVersion)

	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, started[0].ExecutionId, finished[0].ExecutionId)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS, finished[0].Status)
	assert.Equal(t, []byte("ok"), finished[0].OutputInline)
	assert.False(t, finished[0].OutputStreamed)

	require.Equal(t, 1, scheduler.cache.Len())
	cached := scheduler.cache.Get(7)
	require.NotNil(t, cached)
	assert.Equal(t, 1, cached.Counter(), "successful run should increment counter")
}

func TestTick_DisabledTask_NotFired(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	loader := &fakeLoader{cmd: &fakeCommand{}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	scheduler.cache.Put(domain.NewServerTask(domain.ServerTaskOptions{
		ID:           1,
		ServerID:     42,
		Version:      1,
		Command:      domain.ServerTaskRestart,
		ExecuteDate:  now.Add(-time.Minute),
		RepeatPeriod: time.Hour,
		Enabled:      false,
		Server:       server,
	}))

	scheduler.tick(context.Background())

	assert.Empty(t, sender.Started())
	assert.Empty(t, sender.Finished())
	assert.Equal(t, 0, loader.Calls())
}

func TestTick_FutureTask_NotFired(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	loader := &fakeLoader{cmd: &fakeCommand{}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	scheduler.cache.Put(domain.NewServerTask(domain.ServerTaskOptions{
		ID:           1,
		ServerID:     42,
		Version:      1,
		Command:      domain.ServerTaskRestart,
		ExecuteDate:  now.Add(time.Minute),
		RepeatPeriod: time.Hour,
		Enabled:      true,
		Server:       server,
	}))

	scheduler.tick(context.Background())

	assert.Empty(t, sender.Started())
	assert.Equal(t, 0, loader.Calls())
}

// waitForFinished polls the sender until at least n Finished events are seen
// or 2 s elapses (the execution goroutine is async).
func waitForFinished(t *testing.T, s *fakeSender, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.Finished()) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("expected %d Finished events; got %d", n, len(s.Finished()))
}

// waitForStarted polls until n Started events appear.
func waitForStarted(t *testing.T, s *fakeSender, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.Started()) >= n {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("expected %d Started events; got %d", n, len(s.Started()))
}
