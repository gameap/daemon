package mocks

import (
	"context"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

type ServerTaskRepository struct {
	items map[int]*domain.ServerTask
	fails map[int][][]byte
	mutex sync.Mutex
}

func NewServerTaskRepository() *ServerTaskRepository {
	return &ServerTaskRepository{
		items: make(map[int]*domain.ServerTask),
		fails: make(map[int][][]byte),
	}
}

func (r *ServerTaskRepository) Find(ctx context.Context) ([]*domain.ServerTask, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	items := make([]*domain.ServerTask, 0, len(r.items))

	for _, v := range r.items {
		items = append(items, v)
	}

	return items, nil
}

func (r *ServerTaskRepository) FindByID(ctx context.Context, id int) (*domain.ServerTask, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	item, exists := r.items[id]
	if !exists {
		return nil, nil
	}

	return item, nil
}

func (r *ServerTaskRepository) Save(ctx context.Context, task *domain.ServerTask) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items[task.ID()] = task

	return nil
}

func (r *ServerTaskRepository) Fail(ctx context.Context, task *domain.ServerTask, output []byte) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	fails, ok := r.fails[task.ID()]
	if !ok {
		r.fails[task.ID()] = [][]byte{}
		fails = r.fails[task.ID()]
	}

	fails = append(fails, output)

	r.fails[task.ID()] = fails

	return nil
}

func (r *ServerTaskRepository) Set(items []*domain.ServerTask) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, v := range items {
		r.items[v.ID()] = v
	}
}

func (r *ServerTaskRepository) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items = map[int]*domain.ServerTask{}
	r.fails = make(map[int][][]byte)
}
