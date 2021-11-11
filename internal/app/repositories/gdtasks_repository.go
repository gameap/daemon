package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/pkg/errors"
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

func (repository *GDTasksRepository) FindByStatus(
	ctx context.Context,
	status domain.GDTaskStatus,
) ([]*domain.GDTask, error) {
	resp, err := repository.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "/gdaemon_api/tasks",
		QueryParams: map[string]string{
			"filter[status]": string(status),
			"append":         "status_num",
		},
	})

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
	resp, err := repository.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "/gdaemon_api/tasks/{id}",
		PathParams: map[string]string{
			"id": strconv.Itoa(id),
		},
	})

	if err != nil {
		return nil, err
	}

	var tsk task

	err = json.Unmarshal(resp.Body(), &tsk)
	if err != nil {
		return nil, err
	}

	server, err := repository.serverRepository.FindByID(ctx, tsk.Server)
	if err != nil {
		return nil, err
	}

	return domain.NewGDTask(
		tsk.ID,
		tsk.RunAfterID,
		server,
		domain.GDTaskCommand(tsk.Task),
		tsk.Cmd,
		domain.GDTaskStatus(tsk.Status),
	), nil
}

func (repository *GDTasksRepository) Save(ctx context.Context, gdtask *domain.GDTask) error {
	marshalled, err := json.Marshal(struct {
		Status uint8 `json:"status"`
	}{gdtask.StatusNum()})
	if err != nil {
		return errors.WithMessage(err, "failed to marshal gd task")
	}

	resp, err := repository.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPut,
		URL:    "/gdaemon_api/tasks/{id}",
		Body:   marshalled,
		PathParams: map[string]string{
			"id": strconv.Itoa(gdtask.ID()),
		},
	})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body())
	}

	return nil
}

func (repository *GDTasksRepository) AppendOutput(ctx context.Context, gdtask *domain.GDTask, output []byte) error {
	marshalled, err := json.Marshal(struct {
		Output string `json:"output"`
	}{string(output)})
	if err != nil {
		return errors.WithMessage(err, "failed to marshal output")
	}

	resp, err := repository.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPut,
		URL:    "/gdaemon_api/tasks/{id}/output",
		Body:   marshalled,
		PathParams: map[string]string{
			"id": strconv.Itoa(gdtask.ID()),
		},
	})
	if err != nil {
		return err
	}

	if resp.StatusCode() != http.StatusOK {
		return NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body())
	}

	return nil
}
