package serversscheduler

import (
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

type taskCache struct {
	mu    sync.Mutex
	tasks map[uint64]*domain.ServerTask
}

func newTaskCache() *taskCache {
	return &taskCache{tasks: make(map[uint64]*domain.ServerTask)}
}

func (c *taskCache) Get(id uint64) *domain.ServerTask {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.tasks[id]
}

func (c *taskCache) Put(t *domain.ServerTask) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks[t.ID()] = t
}

func (c *taskCache) Delete(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.tasks, id)
}

func (c *taskCache) Replace(tasks []*domain.ServerTask) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tasks = make(map[uint64]*domain.ServerTask, len(tasks))
	for _, t := range tasks {
		c.tasks[t.ID()] = t
	}
}

func (c *taskCache) Snapshot() []*domain.ServerTask {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]*domain.ServerTask, 0, len(c.tasks))
	for _, t := range c.tasks {
		out = append(out, t)
	}

	return out
}

func (c *taskCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.tasks)
}
