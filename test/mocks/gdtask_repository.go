package mocks

import (
	"context"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

type GDTaskRepository struct {
	items map[int]*domain.GDTask
	mutex *sync.Mutex
}

func NewGDTaskRepository() *GDTaskRepository {
	return &GDTaskRepository{
		items: make(map[int]*domain.GDTask),
		mutex: &sync.Mutex{},
	}
}

func (r *GDTaskRepository) FindByStatus(_ context.Context, status domain.GDTaskStatus) ([]*domain.GDTask, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	var result []*domain.GDTask

	for _, v := range r.items {
		if v.Status() == status {
			result = append(result, v)
		}
	}

	return result, nil
}

func (r *GDTaskRepository) FindByID(_ context.Context, id int) (*domain.GDTask, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, v := range r.items {
		if v.ID() == id {
			return v, nil
		}
	}

	return nil, nil
}

func (r *GDTaskRepository) Save(_ context.Context, task *domain.GDTask) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items[task.ID()] = task

	return nil
}

func (r *GDTaskRepository) AppendOutput(_ context.Context, _ *domain.GDTask, _ []byte) error {
	return nil
}

func (r *GDTaskRepository) Set(items []*domain.GDTask) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, v := range items {
		r.items[v.ID()] = v
	}
}

func (r *GDTaskRepository) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items = map[int]*domain.GDTask{}
}
