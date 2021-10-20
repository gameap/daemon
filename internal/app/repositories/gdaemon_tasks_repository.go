package repositories

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/go-resty/resty/v2"
)

type GDTasksRepository struct {
	client *resty.Client
}

func NewGDTasksRepository(client *resty.Client) *GDTasksRepository {
	return &GDTasksRepository{
		client: client,
	}
}

func (repository *GDTasksRepository) FindByStatus(ctx context.Context, status domain.GDTaskStatus) ([]*domain.GDTask, error) {
	resp, err := repository.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"filter[status]": string(status),
			"append":         "status_num",
		}).
		SetHeader("Accept", "application/json").
		Get("/gdaemon_api/tasks")

	if err != nil {
		return nil, err
	}

	var tasks []*domain.GDTask

	err = json.Unmarshal(resp.Body(), &tasks)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (repository *GDTasksRepository) FindByID(ctx context.Context, id int) (*domain.GDTask, error) {
	resp, err := repository.client.R().
		SetContext(ctx).
		SetPathParams(map[string]string{"id": strconv.Itoa(id)}).
		SetHeader("Accept", "application/json").
		Get("/gdaemon_api/tasks/{id}")

	if err != nil {
		return nil, err
	}

	var task domain.GDTask

	err = json.Unmarshal(resp.Body(), &task)
	if err != nil {
		return nil, err
	}

	return &task, nil
}

func (repository *GDTasksRepository) Save(ctx context.Context, task *domain.GDTask) error {
	panic("implement me")
}
