package gdaemonscheduler

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
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
