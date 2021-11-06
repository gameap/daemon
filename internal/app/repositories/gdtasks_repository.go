package repositories

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type GDTasksRepository struct {
	client           interfaces.APIRequestMaker
	serverRepository domain.ServerRepository
}

type task struct {
	ID         int    `json:"id"`
	RunAfterID int    `json:"run_after_id"`
	Server     int    `json:"server_id"`
	Task       string `json:"task"`
	Cmd        string `json:"cmd"`
	Status     string `json:"status"`
}

func NewGDTasksRepository(
	client interfaces.APIRequestMaker,
	serverRepository domain.ServerRepository,
) *GDTasksRepository {
	return &GDTasksRepository{
		client:           client,
		serverRepository: serverRepository,
	}
}

func (repository *GDTasksRepository) FindByStatus(ctx context.Context, status domain.GDTaskStatus) ([]*domain.GDTask, error) {
	resp, err := repository.client.Request().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"filter[status]": string(status),
			"append":         "status_num",
		}).
		Get("/gdaemon_api/tasks")

	if err != nil {
		return nil, err
	}

	var tsk []task
	err = json.Unmarshal(resp.Body(), &tsk)
	if err != nil {
		return nil, err
	}

	var tasks []*domain.GDTask
	for i := range tsk {
		server, err := repository.serverRepository.FindByID(ctx, tsk[i].Server)
		if err != nil {
			return nil, err
		}

		gdTask := domain.NewGDTask(
			tsk[i].ID,
			tsk[i].RunAfterID,
			server,
			domain.GDTaskCommand(tsk[i].Task),
			tsk[i].Cmd,
			domain.GDTaskStatus(tsk[i].Status),
		)

		tasks = append(tasks, gdTask)
	}

	return tasks, nil
}

func (repository *GDTasksRepository) FindByID(ctx context.Context, id int) (*domain.GDTask, error) {
	resp, err := repository.client.Request().
		SetContext(ctx).
		SetPathParams(map[string]string{"id": strconv.Itoa(id)}).
		SetHeader("Accept", "application/json").
		Get("/gdaemon_api/tasks/{id}")

	if err != nil {
		return nil, err
	}

	var gdTask domain.GDTask

	err = json.Unmarshal(resp.Body(), &gdTask)
	if err != nil {
		return nil, err
	}

	return &gdTask, nil
}

func (repository *GDTasksRepository) Save(ctx context.Context, task *domain.GDTask) error {
	panic("implement me")
}
