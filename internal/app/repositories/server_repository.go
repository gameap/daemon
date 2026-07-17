package repositories

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

type ServerRepository struct {
	servers     sync.Map
	lastUpdated sync.Map
}

func NewServerRepository() *ServerRepository {
	return &ServerRepository{}
}

func (repo *ServerRepository) IDsFromCache() []int {
	var ids []int
	repo.servers.Range(func(key, _ interface{}) bool {
		if id, ok := key.(int); ok {
			ids = append(ids, id)
		}
		return true
	})
	return ids
}

func (repo *ServerRepository) IDs(_ context.Context) ([]int, error) {
	return repo.IDsFromCache(), nil
}

func (repo *ServerRepository) FindByID(_ context.Context, id int) (*domain.Server, error) {
	server, ok := repo.FindByIDFromCache(id)
	if !ok {
		return nil, nil
	}

	return server, nil
}

func (repo *ServerRepository) Save(_ context.Context, _ *domain.Server) error {
	return nil
}

func (repo *ServerRepository) FindByIDFromCache(id int) (*domain.Server, bool) {
	loaded, ok := repo.servers.Load(id)
	if !ok {
		return nil, false
	}

	return loaded.(*domain.Server), true
}

func (repo *ServerRepository) SaveToCache(server *domain.Server) {
	repo.servers.Store(server.ID(), server)
	repo.lastUpdated.Store(server.ID(), time.Now())
}

func (repo *ServerRepository) CountOnlineServers() int {
	count := 0
	repo.servers.Range(func(_, value interface{}) bool {
		if server, ok := value.(*domain.Server); ok && server.IsActive() {
			count++
		}
		return true
	})
	return count
}
