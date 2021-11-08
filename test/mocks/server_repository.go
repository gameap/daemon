package mocks

import (
	"context"
	"sync"

	"github.com/gameap/daemon/internal/app/domain"
)

type ServerRepository struct {
	items map[int]*domain.Server
	mutex sync.Mutex
}

func NewServerRepository() *ServerRepository {
	return &ServerRepository{
		items: make(map[int]*domain.Server),
	}
}

func (r *ServerRepository) IDs(ctx context.Context) ([]int, error) {
	ids := make([]int, 0, len(r.items))

	for _, v := range r.items {
		ids = append(ids, v.ID())
	}

	return ids, nil
}

func (r *ServerRepository) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, v := range r.items {
		if v.ID() == id {
			return v, nil
		}
	}

	return nil, nil
}

func (r *ServerRepository) Save(ctx context.Context, server *domain.Server) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items[server.ID()] = server

	return nil
}

func (r *ServerRepository) Set(items []*domain.Server) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	for _, v := range items {
		r.items[v.ID()] = v
	}
}

func (r *ServerRepository) Clear() {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.items = map[int]*domain.Server{}
}
