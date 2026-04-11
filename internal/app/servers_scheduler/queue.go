package serversscheduler

import (
	"sort"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

type taskQueue struct {
	tasks []*domain.ServerTask
	ids   map[int]struct{}
	mutex *sync.Mutex
}

func newTaskQueue() *taskQueue {
	return &taskQueue{
		tasks: make([]*domain.ServerTask, 0),
		ids:   make(map[int]struct{}),
		mutex: &sync.Mutex{},
	}
}

func (q *taskQueue) Exists(task *domain.ServerTask) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	_, exists := q.ids[task.ID()]

	return exists
}

func (q *taskQueue) Replace(task *domain.ServerTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	for i, t := range q.tasks {
		if t.ID() == task.ID() {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)

			break
		}
	}

	q.insertSorted(task)
}

func (q *taskQueue) Put(task *domain.ServerTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if _, exists := q.ids[task.ID()]; exists {
		return
	}

	q.ids[task.ID()] = struct{}{}
	q.insertSorted(task)
}

// Pop returns the earliest task without removing it from the queue.
func (q *taskQueue) Pop() *domain.ServerTask {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.tasks) == 0 {
		return nil
	}

	return q.tasks[0]
}

func (q *taskQueue) Remove(task *domain.ServerTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	for i, t := range q.tasks {
		if t.ID() == task.ID() {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			delete(q.ids, task.ID())

			return
		}
	}
}

func (q *taskQueue) Empty() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return len(q.tasks) == 0
}

func (q *taskQueue) insertSorted(task *domain.ServerTask) {
	executeDate := task.ExecuteDate()
	i := sort.Search(len(q.tasks), func(j int) bool {
		return q.tasks[j].ExecuteDate().After(executeDate)
	})

	q.tasks = append(q.tasks, nil)
	copy(q.tasks[i+1:], q.tasks[i:])
	q.tasks[i] = task
}
