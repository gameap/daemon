package serversscheduler

import (
	"context"
	"testing"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func runScheduledCommandOnce(
	t *testing.T, command pb.ServerTaskCommand, suspended bool,
) (*fakeSender, *fakeLoader) {
	t.Helper()

	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	server := newServerForTaskWithState(42, suspended)
	loader := &fakeLoader{cmd: &fakeCommand{output: []byte("ok")}}
	sender := newFakeSender()
	scheduler := newTestScheduler(loader, newFakeServerRepo(server), sender)
	freezeTime(scheduler, now)

	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            7,
			ServerId:      42,
			Version:       1,
			Command:       command,
			ExecuteDate:   timestamppb.New(now.Add(-time.Second)),
			RepeatPeriod:  durationpb.New(time.Hour),
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
			Enabled:       true,
		}},
	})

	scheduler.tick(context.Background())
	waitForFinished(t, sender, 1)

	return sender, loader
}

// A suspended server is not brought up, updated or reinstalled by a schedule the
// operator set up before the suspension. The run is reported as skipped rather
// than failed: nothing went wrong, and the schedule carries on for when the
// suspension is lifted.
func TestExecuteNow_SuspendedServerSkipsCommandsThatWouldStartIt(t *testing.T) {
	tests := []struct {
		name    string
		command pb.ServerTaskCommand
	}{
		{name: "start", command: pb.ServerTaskCommand_SERVER_TASK_COMMAND_START},
		{name: "restart", command: pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART},
		{name: "update", command: pb.ServerTaskCommand_SERVER_TASK_COMMAND_UPDATE},
		{name: "reinstall", command: pb.ServerTaskCommand_SERVER_TASK_COMMAND_REINSTALL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender, loader := runScheduledCommandOnce(t, tt.command, true)

			started := sender.Started()
			require.Len(t, started, 1)
			finished := sender.Finished()
			require.Len(t, finished, 1)
			assert.Equal(t, started[0].ExecutionId, finished[0].ExecutionId)
			assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SKIPPED, finished[0].Status)
			assert.Equal(t, "server is blocked", finished[0].ErrorMessage)
			assert.Empty(t, finished[0].OutputInline)
			assert.Equal(t, 0, loader.Calls(), "no command is even built for a skipped run")
		})
	}
}

func TestExecuteNow_SuspendedServerStillRunsScheduledStop(t *testing.T) {
	sender, loader := runScheduledCommandOnce(t, pb.ServerTaskCommand_SERVER_TASK_COMMAND_STOP, true)

	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS, finished[0].Status)
	assert.Empty(t, finished[0].ErrorMessage)
	assert.Equal(t, []byte("ok"), finished[0].OutputInline)
	assert.Equal(t, 1, loader.Calls())
}

func TestExecuteNow_NotSuspendedServerRunsScheduledStart(t *testing.T) {
	sender, loader := runScheduledCommandOnce(t, pb.ServerTaskCommand_SERVER_TASK_COMMAND_START, false)

	finished := sender.Finished()
	require.Len(t, finished, 1)
	assert.Equal(t, pb.ServerTaskExecutionStatus_SERVER_TASK_EXECUTION_STATUS_SUCCESS, finished[0].Status)
	assert.Equal(t, 1, loader.Calls())
}
