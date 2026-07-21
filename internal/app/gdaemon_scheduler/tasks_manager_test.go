package gdaemonscheduler

import (
	"context"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_taskQueue(t *testing.T) {
	queue := newTaskQueue()
	assert.Len(t, queue.tasks, 0)

	task1 := domain.NewGDTask(1, 0, nil, "", "", "")
	task2 := domain.NewGDTask(2, 0, nil, "", "", "")

	queue.Insert([]*domain.GDTask{task1, task2})
	assert.Len(t, queue.tasks, 2)
	assert.Equal(t, queue.Next(), task1)
	assert.Len(t, queue.tasks, 2)

	require.Equal(t, queue.Dequeue(), task2)
	require.Len(t, queue.tasks, 1)

	go func() {
		queue.Insert([]*domain.GDTask{
			domain.NewGDTask(2, 0, nil, "", "", ""),
			domain.NewGDTask(3, 0, nil, "", "", ""),
			domain.NewGDTask(4, 0, nil, "", "", ""),
		})
	}()

	time.Sleep(100 * time.Millisecond)

	queue.Insert([]*domain.GDTask{
		domain.NewGDTask(5, 0, nil, "", "", ""),
		domain.NewGDTask(6, 0, nil, "", "", ""),
		domain.NewGDTask(7, 0, nil, "", "", ""),
	})

	f := queue.FindByID(7)
	assert.Equal(t, f.ID(), 7)

	queue.Remove(task1)
	assert.Len(t, queue.tasks, 6)
}

func Test_checkPredecessor(t *testing.T) {
	newManager := func(timeout time.Duration) *TaskManager {
		manager := NewTaskManager(nil, nil, nil, &config.Config{})
		manager.predecessorMissingTimeout = timeout

		return manager
	}

	t.Run("no predecessor proceeds", func(t *testing.T) {
		manager := newManager(time.Minute)
		task := domain.NewGDTask(1, 0, nil, "", "", domain.GDTaskStatusWaiting)

		decision, reason := manager.checkPredecessor(context.Background(), task)

		assert.Equal(t, predecessorProceed, decision)
		assert.Empty(t, reason)
	})

	t.Run("missing predecessor waits and then fails", func(t *testing.T) {
		manager := newManager(50 * time.Millisecond)
		task := domain.NewGDTask(1, 2, nil, "", "", domain.GDTaskStatusWaiting)

		decision, reason := manager.checkPredecessor(context.Background(), task)
		require.Equal(t, predecessorWait, decision)
		require.Empty(t, reason)

		time.Sleep(60 * time.Millisecond)

		decision, reason = manager.checkPredecessor(context.Background(), task)

		assert.Equal(t, predecessorFail, decision)
		assert.Contains(t, reason, "predecessor task 2 not found")
	})

	t.Run("predecessor arriving before the timeout resets the wait", func(t *testing.T) {
		manager := newManager(50 * time.Millisecond)
		task := domain.NewGDTask(1, 2, nil, "", "", domain.GDTaskStatusWaiting)

		decision, _ := manager.checkPredecessor(context.Background(), task)
		require.Equal(t, predecessorWait, decision)

		manager.completed.Record(2, domain.GDTaskStatusSuccess)
		decision, _ = manager.checkPredecessor(context.Background(), task)
		require.Equal(t, predecessorProceed, decision)

		_, waiting := manager.predecessorWaits.Load(task.ID())
		assert.False(t, waiting, "the wait deadline must be dropped once the predecessor is found")
	})

	t.Run("failed predecessor fails the task", func(t *testing.T) {
		manager := newManager(time.Minute)
		task := domain.NewGDTask(1, 2, nil, "", "", domain.GDTaskStatusWaiting)
		manager.completed.Record(2, domain.GDTaskStatusError)

		decision, reason := manager.checkPredecessor(context.Background(), task)

		assert.Equal(t, predecessorFail, decision)
		assert.Contains(t, reason, "predecessor task 2 failed")
	})
}

func Test_proceedTask_SendsFinalOutputBeforeStatus(t *testing.T) {
	tests := []struct {
		name           string
		result         int
		expectedStatus string
	}{
		{"failed command", 1, string(domain.GDTaskStatusError)},
		{"successful command", gameservercommands.SuccessResult, string(domain.GDTaskStatusSuccess)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewTaskManager(nil, nil, nil, &config.Config{})
			sender := &recordingTaskStatusSender{}
			manager.SetTaskStatusSender(sender)

			task := domain.NewGDTask(1, 0, nil, "", "", domain.GDTaskStatusWorking)
			manager.commandsInProgress.Store(task.ID(), &completedCommand{
				output: []byte("last output line"),
				result: tt.result,
			})

			require.NoError(t, manager.proceedTask(context.Background(), task))

			require.Equal(t, []string{"output:last output line:final", "status:" + tt.expectedStatus}, sender.events)
		})
	}
}

type recordingTaskStatusSender struct {
	events []string
}

func (s *recordingTaskStatusSender) SendTaskStatus(_ int, status string, _ string) {
	s.events = append(s.events, "status:"+status)
}

func (s *recordingTaskStatusSender) SendTaskOutput(_ int, output []byte, isFinal bool) {
	event := "output:" + string(output)
	if isFinal {
		event += ":final"
	}

	s.events = append(s.events, event)
}

type completedCommand struct {
	output []byte
	result int
}

func (c *completedCommand) ReadOutput() []byte { return c.output }
func (c *completedCommand) Result() int        { return c.result }
func (c *completedCommand) IsComplete() bool   { return true }
