package serversscheduler

import (
	"testing"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCatchup_Skip_OnSnapshotApply_ShiftsToNextSlot(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(server), newFakeSender())
	freezeTime(scheduler, now)

	original := now.Add(-25 * time.Minute)
	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            1,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(original),
			RepeatPeriod:  durationpb.New(10 * time.Minute),
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
			Enabled:       true,
		}},
	})

	cached := scheduler.cache.Get(1)
	require.NotNil(t, cached)
	assert.Equal(t, original.Add(30*time.Minute), cached.ExecuteDate())
}

func TestCatchup_RunOnce_OnSnapshotApply_ShiftsToNow(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(server), newFakeSender())
	freezeTime(scheduler, now)

	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            1,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(now.Add(-2 * time.Hour)),
			RepeatPeriod:  durationpb.New(10 * time.Minute),
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_RUN_ONCE,
			Enabled:       true,
		}},
	})

	cached := scheduler.cache.Get(1)
	require.NotNil(t, cached)
	assert.Equal(t, now, cached.ExecuteDate())
}

func TestCatchup_WithinGracePeriod_NotShifted(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(server), newFakeSender())
	freezeTime(scheduler, now)

	executeDate := now.Add(-30 * time.Second)
	scheduler.ApplySnapshot(&pb.ServerTaskSnapshot{
		Tasks: []*pb.ServerTask{{
			Id:            1,
			ServerId:      42,
			Version:       1,
			Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
			ExecuteDate:   timestamppb.New(executeDate),
			RepeatPeriod:  durationpb.New(10 * time.Minute),
			OverlapPolicy: pb.ServerTaskOverlapPolicy_SERVER_TASK_OVERLAP_POLICY_SKIP,
			CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
			Enabled:       true,
		}},
	})

	cached := scheduler.cache.Get(1)
	require.NotNil(t, cached)
	assert.Equal(t, executeDate, cached.ExecuteDate(), "task within 1-minute grace period must keep original date")
}

func TestApplyDelta_NewTask_NotInGrace_GetsCatchup(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	server := newServerForTask(42)
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(server), newFakeSender())
	freezeTime(scheduler, now)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Upserted{
			Upserted: &pb.ServerTask{
				Id:            42,
				ServerId:      42,
				Version:       1,
				Command:       pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
				ExecuteDate:   timestamppb.New(now.Add(-25 * time.Minute)),
				RepeatPeriod:  durationpb.New(10 * time.Minute),
				CatchupPolicy: pb.ServerTaskCatchupPolicy_SERVER_TASK_CATCHUP_POLICY_SKIP,
				Enabled:       true,
			},
		},
	})

	cached := scheduler.cache.Get(42)
	require.NotNil(t, cached)
	assert.True(t, !cached.ExecuteDate().Before(now), "delta upsert with SKIP catchup must produce future executeDate")
}
