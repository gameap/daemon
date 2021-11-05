package repositories

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type ServerRepository struct {
	client interfaces.APIRequestMaker
}

func NewServerRepository(client interfaces.APIRequestMaker) *ServerRepository {
	return &ServerRepository{
		client: client,
	}
}

func (repo *ServerRepository) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	resp, err := repo.client.Request().
		SetContext(ctx).
		Get("/gdaemon_api/tasks")

	if err != nil {
		return nil, err
	}
}

func (repo *ServerRepository) Save(ctx context.Context, task *domain.Server) error {
	panic("implement me")
}
