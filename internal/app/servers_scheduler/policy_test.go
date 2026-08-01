package serversscheduler

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

func TestNextFireAfter_FutureDate_ReturnsAsIs(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Second)

	got := nextFireAfter(future, time.Minute, domain.ServerTaskCatchupSkip, now)

	assert.Equal(t, future, got)
}

func TestNextFireAfter_SkipPolicy_JumpsForwardByWholePeriods(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-25 * time.Minute)

	got := nextFireAfter(executeDate, 10*time.Minute, domain.ServerTaskCatchupSkip, now)

	assert.Equal(t, executeDate.Add(30*time.Minute), got)
	assert.True(t, !got.Before(now), "skip catchup must produce a slot >= now")
}

func TestNextFireAfter_RunOncePolicy_ReturnsNow(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-2 * time.Hour)

	got := nextFireAfter(executeDate, 10*time.Minute, domain.ServerTaskCatchupRunOnce, now)

	assert.Equal(t, now, got)
}

func TestNextFireAfter_NonRepeating_SkipPolicy_ReturnsZeroTime(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-time.Hour)

	got := nextFireAfter(executeDate, 0, domain.ServerTaskCatchupSkip, now)

	assert.True(t, got.IsZero(), "non-repeating + SKIP catchup must yield zero time")
}

func TestNextFireAfter_NonRepeating_RunOnce_ReturnsNow(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-time.Hour)

	got := nextFireAfter(executeDate, 0, domain.ServerTaskCatchupRunOnce, now)

	assert.Equal(t, now, got)
}

func TestApplyCatchupOnApply_OnlyLateTasksAreShifted(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-30 * time.Second)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            1,
		ExecuteDate:   executeDate,
		RepeatPeriod:  10 * time.Minute,
		CatchupPolicy: domain.ServerTaskCatchupSkip,
		Enabled:       true,
	})

	applyCatchupOnApply(task, now)

	assert.Equal(t, executeDate, task.ExecuteDate(), "task within grace period must be untouched")
}

func TestApplyCatchupOnApply_LateSkipShiftsForward(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-25 * time.Minute)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            1,
		ExecuteDate:   executeDate,
		RepeatPeriod:  10 * time.Minute,
		CatchupPolicy: domain.ServerTaskCatchupSkip,
		Enabled:       true,
	})

	applyCatchupOnApply(task, now)

	assert.Equal(t, executeDate.Add(30*time.Minute), task.ExecuteDate())
}

func TestApplyCatchupOnApply_DisabledTask_NotShifted(t *testing.T) {
	now := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	executeDate := now.Add(-1 * time.Hour)

	task := domain.NewServerTask(domain.ServerTaskOptions{
		ID:            1,
		ExecuteDate:   executeDate,
		RepeatPeriod:  10 * time.Minute,
		CatchupPolicy: domain.ServerTaskCatchupSkip,
		Enabled:       false,
	})

	applyCatchupOnApply(task, now)

	assert.Equal(t, executeDate, task.ExecuteDate(), "disabled task must not be shifted")
}
