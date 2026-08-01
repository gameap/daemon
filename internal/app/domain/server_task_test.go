package domain

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestServerTask_ConcurrentUpdateAndRead guards the scheduler access pattern:
// the gRPC stream applies snapshots and deltas while the scheduler tick reads
// the same task. Every getter must take the task mutex, so this fails under
// -race when one of them reads a field lock-free.
func TestServerTask_ConcurrentUpdateAndRead(t *testing.T) {
	task := NewServerTask(ServerTaskOptions{
		ID:            1,
		ServerID:      10,
		NodeID:        20,
		Version:       1,
		Command:       ServerTaskStart,
		ExecuteDate:   time.Now(),
		Repeat:        5,
		RepeatPeriod:  time.Minute,
		OverlapPolicy: ServerTaskOverlapSkip,
		CatchupPolicy: ServerTaskCatchupSkip,
		Name:          "task",
		Timezone:      "UTC",
		Payload:       "payload",
		Enabled:       true,
		UpdatedAt:     time.Now(),
	})

	const iterations = 500

	// Each getter gets its own goroutine: a reader calling every getter in one
	// loop would order itself against the writer through the locked getters and
	// hide a lock-free one from the race detector.
	readers := []func(){
		func() { _ = task.ID() },
		func() { _ = task.ServerID() },
		func() { _ = task.NodeID() },
		func() { _ = task.Version() },
		func() { _ = task.Command() },
		func() { _ = task.Server() },
		func() { _ = task.Repeat() },
		func() { _ = task.RepeatPeriod() },
		func() { _ = task.ExecuteDate() },
		func() { _ = task.Counter() },
		func() { _ = task.OverlapPolicy() },
		func() { _ = task.CatchupPolicy() },
		func() { _ = task.Name() },
		func() { _ = task.Timezone() },
		func() { _ = task.Payload() },
		func() { _ = task.Enabled() },
		func() { _ = task.UpdatedAt() },
		func() { _ = task.RepeatEndlessly() },
		func() { _ = task.CanExecute() },
		func() { _ = task.IsActive() },
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for i := range iterations {
			task.UpdateFromOptions(ServerTaskOptions{
				ID:            1,
				ServerID:      uint64(i),
				NodeID:        uint64(i),
				Version:       uint64(i),
				Command:       ServerTaskRestart,
				Server:        &Server{},
				ExecuteDate:   time.Now(),
				Repeat:        i,
				RepeatPeriod:  time.Duration(i) * time.Second,
				Counter:       i,
				OverlapPolicy: ServerTaskOverlapQueue,
				CatchupPolicy: ServerTaskCatchupRunOnce,
				Name:          "updated",
				Timezone:      "Europe/Moscow",
				Payload:       "updated payload",
				Enabled:       i%2 == 0,
				UpdatedAt:     time.Now(),
			})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for range iterations {
			task.IncreaseCountersAndTime()
		}
	}()

	for _, read := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range iterations {
				read()
			}
		}()
	}

	wg.Wait()
}

func TestServerTask_IsActive(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		repeat   int
		counter  int
		expected bool
	}{
		{"endless repeat", true, 0, 100, true},
		{"infinite repeat", true, -1, 100, true},
		{"repeats left", true, 3, 2, true},
		{"repeats exhausted", true, 3, 3, false},
		{"disabled", false, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := NewServerTask(ServerTaskOptions{
				ID:      1,
				Repeat:  tt.repeat,
				Counter: tt.counter,
				Enabled: tt.enabled,
			})

			assert.Equal(t, tt.expected, task.IsActive())
		})
	}
}
