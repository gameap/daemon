package gdaemonscheduler

import (
	"testing"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

func Test_completionTracker_RecordAndStatus(t *testing.T) {
	tracker := newCompletionTracker(8)

	tracker.Record(1, domain.GDTaskStatusSuccess)
	tracker.Record(2, domain.GDTaskStatusError)

	status, ok := tracker.Status(1)
	assert.True(t, ok)
	assert.Equal(t, domain.GDTaskStatusSuccess, status)

	status, ok = tracker.Status(2)
	assert.True(t, ok)
	assert.Equal(t, domain.GDTaskStatusError, status)

	_, ok = tracker.Status(99)
	assert.False(t, ok)
}

func Test_completionTracker_EvictionFIFO(t *testing.T) {
	tracker := newCompletionTracker(3)

	tracker.Record(1, domain.GDTaskStatusSuccess)
	tracker.Record(2, domain.GDTaskStatusSuccess)
	tracker.Record(3, domain.GDTaskStatusSuccess)
	tracker.Record(4, domain.GDTaskStatusError)

	_, ok := tracker.Status(1)
	assert.False(t, ok, "oldest entry should be evicted")

	for _, id := range []int{2, 3, 4} {
		_, ok := tracker.Status(id)
		assert.True(t, ok, "expected id %d to be present", id)
	}
}

func Test_completionTracker_UpdateExisting(t *testing.T) {
	tracker := newCompletionTracker(3)

	tracker.Record(1, domain.GDTaskStatusWorking)
	tracker.Record(2, domain.GDTaskStatusSuccess)
	tracker.Record(3, domain.GDTaskStatusSuccess)

	tracker.Record(1, domain.GDTaskStatusSuccess)

	tracker.Record(4, domain.GDTaskStatusSuccess)

	status, ok := tracker.Status(1)
	assert.False(t, ok, "id 1 should be evicted as it was first inserted")
	assert.Empty(t, status)

	for _, id := range []int{2, 3, 4} {
		_, ok := tracker.Status(id)
		assert.True(t, ok)
	}
}

func Test_completionTracker_DefaultCapacity(t *testing.T) {
	tracker := newCompletionTracker(0)
	assert.Equal(t, completionTrackerCapacity, tracker.capacity)
}
