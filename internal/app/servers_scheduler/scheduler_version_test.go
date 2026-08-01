package serversscheduler

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func seedCache(t *testing.T, s *Scheduler, id, version uint64) {
	t.Helper()
	server := newServerForTask(int(id))
	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:           id,
		ServerID:     42,
		Version:      version,
		Command:      domain.ServerTaskRestart,
		ExecuteDate:  s.now().Add(time.Hour),
		RepeatPeriod: time.Hour,
		Enabled:      true,
		Server:       server,
	})
	s.cache.Put(task)
}

func TestApplyDelta_StaleVersion_Dropped_NoResync(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	seedCache(t, scheduler, 1, 5)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Upserted{
			Upserted: &pb.ServerTask{
				Id:           1,
				ServerId:     42,
				Version:      4,
				Command:      pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
				ExecuteDate:  timestamppb.New(now.Add(time.Hour)),
				RepeatPeriod: durationpb.New(time.Hour),
				Enabled:      true,
			},
		},
	})

	assert.Empty(t, sender.ResyncRequests(), "stale delta must not trigger resync")
	cached := scheduler.cache.Get(1)
	require.NotNil(t, cached)
	assert.Equal(t, uint64(5), cached.Version(), "stale delta must not overwrite cached state")
}

func TestApplyDelta_VersionGap_TriggersResync(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	seedCache(t, scheduler, 1, 5)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Upserted{
			Upserted: &pb.ServerTask{
				Id:           1,
				ServerId:     42,
				Version:      8,
				Command:      pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
				ExecuteDate:  timestamppb.New(now.Add(time.Hour)),
				RepeatPeriod: durationpb.New(time.Hour),
				Enabled:      true,
			},
		},
	})

	assert.Len(t, sender.ResyncRequests(), 1, "version gap must trigger resync")
	cached := scheduler.cache.Get(1)
	require.NotNil(t, cached)
	assert.Equal(t, uint64(8), cached.Version(), "version-gap delta is still applied so the cache catches up")
}

func TestApplyDelta_UnknownTaskWithVersionAboveOne_TriggersResync(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Upserted{
			Upserted: &pb.ServerTask{
				Id:           99,
				ServerId:     42,
				Version:      3,
				Command:      pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
				ExecuteDate:  timestamppb.New(now.Add(time.Hour)),
				RepeatPeriod: durationpb.New(time.Hour),
				Enabled:      true,
			},
		},
	})

	assert.Len(t, sender.ResyncRequests(), 1)
	require.NotNil(t, scheduler.cache.Get(99))
}

func TestApplyDelta_ResyncRateLimit(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	// Seed a cached task at a different id to ensure seedCache is exercised with
	// non-default values (also gives ApplyDelta a known cache state for the gap path).
	seedCache(t, scheduler, 200, 1)

	for i := 0; i < 5; i++ {
		scheduler.ApplyDelta(&pb.ServerTaskDelta{
			Kind: &pb.ServerTaskDelta_Upserted{
				Upserted: &pb.ServerTask{
					Id:           uint64(100 + i),
					ServerId:     42,
					Version:      3,
					Command:      pb.ServerTaskCommand_SERVER_TASK_COMMAND_RESTART,
					ExecuteDate:  timestamppb.New(now.Add(time.Hour)),
					RepeatPeriod: durationpb.New(time.Hour),
					Enabled:      true,
				},
			},
		})
	}

	assert.Len(t, sender.ResyncRequests(), 1, "back-to-back gaps must coalesce to a single resync")
}

func TestApplyDelta_Deleted_RemovesCached(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	seedCache(t, scheduler, 1, 5)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Deleted{
			Deleted: &pb.ServerTaskDeleted{Id: 1, Version: 6},
		},
	})

	assert.Nil(t, scheduler.cache.Get(1))
}

func TestApplyDelta_DeletedStale_Ignored(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	sender := newFakeSender()
	scheduler := newTestScheduler(&fakeLoader{cmd: &fakeCommand{}}, newFakeServerRepo(newServerForTask(42)), sender)
	freezeTime(scheduler, now)

	seedCache(t, scheduler, 1, 5)

	scheduler.ApplyDelta(&pb.ServerTaskDelta{
		Kind: &pb.ServerTaskDelta_Deleted{
			Deleted: &pb.ServerTaskDeleted{Id: 1, Version: 4},
		},
	})

	assert.NotNil(t, scheduler.cache.Get(1))
}
