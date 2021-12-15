package serversscheduler

import (
	"sync"

	"github.com/emirpasic/gods/trees/btree"
	"github.com/emirpasic/gods/utils"
	"github.com/gameap/daemon/internal/app/domain"
)

type taskQueue struct {
	tree  *btree.Tree
	mutex *sync.Mutex
}

func newTaskQueue() *taskQueue {
	return &taskQueue{
		tree:  btree.NewWith(3, utils.TimeComparator),
		mutex: &sync.Mutex{},
	}
}

func (q *taskQueue) Exists(task *domain.ServerTask) bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	_, found := q.tree.Get(task.ExecuteDate())
	q.tree.Values()
	return found
}

func (q *taskQueue) Replace(task *domain.ServerTask) {
	for _, v := range q.tree.Values() {
		t := v.(*domain.ServerTask)

		if t.ID() == task.ID() {
			q.Remove(t)
			q.Put(task)
			return
		}
	}
}

func (q *taskQueue) Put(task *domain.ServerTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.tree.Put(task.ExecuteDate(), task)
}

func (q *taskQueue) Pop() *domain.ServerTask {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	v := q.tree.LeftValue()
	if v == nil {
		return nil
	}

	task := v.(*domain.ServerTask)

	return task
}

func (q *taskQueue) Remove(task *domain.ServerTask) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.tree.Remove(task.ExecuteDate())
}

func (q *taskQueue) Empty() bool {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.tree.Empty()
}
