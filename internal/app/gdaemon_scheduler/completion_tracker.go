package gdaemonscheduler

import (
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

const completionTrackerCapacity = 1024

type completionTracker struct {
	statuses map[int]domain.GDTaskStatus
	order    []int
	capacity int
	mu       sync.RWMutex
}

func newCompletionTracker(capacity int) *completionTracker {
	if capacity <= 0 {
		capacity = completionTrackerCapacity
	}

	return &completionTracker{
		statuses: make(map[int]domain.GDTaskStatus, capacity),
		order:    make([]int, 0, capacity),
		capacity: capacity,
	}
}

func (t *completionTracker) Record(id int, status domain.GDTaskStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.statuses[id]; exists {
		t.statuses[id] = status
		return
	}

	if len(t.order) >= t.capacity {
		evicted := t.order[0]
		t.order = t.order[1:]
		delete(t.statuses, evicted)
	}

	t.statuses[id] = status
	t.order = append(t.order, id)
}

func (t *completionTracker) Status(id int) (domain.GDTaskStatus, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	status, ok := t.statuses[id]
	return status, ok
}
