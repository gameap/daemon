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

func TestOverlap_Skip_SecondFireEmitsSkipped(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	block := make(chan struct{})
	defer close(block)

	loader := &fakeLoader{cmd: &fakeCommand{block: block}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            10,
		ServerID:      42,
		Version:       1,
		Command:       domain.ServerTaskRestart,
		ExecuteDate:   now.Add(-time.Second),
		RepeatPeriod:  time.Hour,
		OverlapPolicy: domain.ServerTaskOverlapSkip,
		Enabled:       true,
		Server:        server,
	})
	scheduler.cache.Put(task)

	scheduler.tick(context.Background())
	waitForStarted(t, sender, 1)

	// Force-rewind executeDate so the second tick re-evaluates the task as due.
	task.SetExecuteDate(now.Add(-time.Second))
	scheduler.tick(context.Background())

	require.Len(t, sender.Started(), 2)
	require.GreaterOrEqual(t, len(sender.Finished()), 1)

	var skipped *pb.ServerTaskExecutionFinished
	for _, f := range sender.Finished() {
		if f.Status == pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SKIPPED {
			skipped = f
			break
		}
	}
	require.NotNil(t, skipped, "expected a SKIPPED Finished event")
	assert.NotEmpty(t, skipped.ErrorMessage)
}

func TestOverlap_Queue_SecondRunStartsAfterFirstCompletes(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	block := make(chan struct{})

	loader := &fakeLoader{cmd: &fakeCommand{block: block}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            11,
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

	// Second arrival is queued: no Started yet, only one Started observed.
	assert.Len(t, sender.Started(), 1, "queued execution must not emit Started yet")

	close(block)

	waitForStarted(t, sender, 2)
	waitForFinished(t, sender, 2)
	assert.Len(t, sender.Finished(), 2)
}
