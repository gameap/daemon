package serversscheduler

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecution_SmallOutput_GoesInline(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	output := []byte("small payload")

	loader := &fakeLoader{cmd: &fakeCommand{output: output}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID: 30, ServerID: 42, Version: 1,
		Command: domain.ServerTaskRestart, Server: server,
		ExecuteDate: now.Add(-time.Second), RepeatPeriod: time.Hour, Enabled: true,
	})
	scheduler.cache.Put(task)

	scheduler.tick(context.Background())
	waitForFinished(t, sender, 1)

	assert.Empty(t, sender.Logs(), "small output should not stream")
	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, output, finished[0].OutputInline)
	assert.False(t, finished[0].OutputStreamed)
	assert.Empty(t, finished[0].OutputStoragePath)
}

func TestExecution_LargeOutput_ChunksAndTrims(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	output := bytes.Repeat([]byte("x"), 200*1024) // 200 KB

	loader := &fakeLoader{cmd: &fakeCommand{output: output}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID: 31, ServerID: 42, Version: 1,
		Command: domain.ServerTaskRestart, Server: server,
		ExecuteDate: now.Add(-time.Second), RepeatPeriod: time.Hour, Enabled: true,
	})
	scheduler.cache.Put(task)

	scheduler.tick(context.Background())
	waitForFinished(t, sender, 1)

	logs := sender.Logs()
	require.NotEmpty(t, logs)
	// Sequence numbers must be monotonic starting at 1.
	for i, l := range logs {
		assert.Equal(t, uint64(i+1), l.Sequence)
	}
	assert.True(t, logs[len(logs)-1].IsFinal, "last log chunk must have IsFinal=true")
	for _, l := range logs[:len(logs)-1] {
		assert.False(t, l.IsFinal)
	}

	rebuilt := []byte{}
	for _, l := range logs {
		rebuilt = append(rebuilt, l.Chunk...)
	}
	assert.Equal(t, output, rebuilt, "chunks reassembled must equal original output")

	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.True(t, finished[0].OutputStreamed)
	assert.Len(t, finished[0].OutputInline, outputInlineMax, "inline tail must be exactly 64KB on streamed output")
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS, finished[0].Status)
}
