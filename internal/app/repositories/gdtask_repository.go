package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/gameap/daemon/internal/app/logger"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type GDTaskRepository struct {
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

func NewGDTaskRepository(
	client interfaces.APIRequestMaker,
	serverRepository domain.ServerRepository,
) *GDTaskRepository {
	return &GDTaskRepository{
		client:           client,
		serverRepository: serverRepository,
	}
}

func (repository *GDTaskRepository) FindByStatus(
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
		return nil, errors.WithMessage(err, "[repositories.GDTaskRepository] failed to find gameap daemon tasks")
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.GDTaskRepository] failed to find gameap daemon tasks",
		)
	}

	var items []task
	err = json.Unmarshal(resp.Body(), &items)
	if err != nil {
		return nil, errors.WithMessage(err, "[repositories.GDTaskRepository] failed to unmarshal gameap daemon tasks")
	}

	tasks := make([]*domain.GDTask, 0, len(items))
	for i := range items {
		server, err := repository.serverRepository.FindByID(ctx, items[i].Server)
		if err != nil {
			return nil, errors.WithMessage(err, "[repositories.GDTaskRepository] failed to join server to gameap daemon task")
		}

		if server == nil {
			logger.WithFields(ctx, log.Fields{
				"gameServerID": items[i].Server,
				"gdTaskID":     items[i].ID,
			}).Warn(ctx, "invalid task, game server not found")
			continue
		}

		gdTask := domain.NewGDTask(
			items[i].ID,
			items[i].RunAfterID,
			server,
			domain.GDTaskCommand(items[i].Task),
			items[i].Cmd,
			domain.GDTaskStatus(items[i].Status),
		)

		tasks = append(tasks, gdTask)
	}

	return tasks, nil
}

func (repository *GDTaskRepository) FindByID(ctx context.Context, id int) (*domain.GDTask, error) {
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

func (repository *GDTaskRepository) Save(ctx context.Context, gdtask *domain.GDTask) error {
	marshalled, err := json.Marshal(struct {
		Status uint8 `json:"status"`
	}{gdtask.StatusNum()})
	if err != nil {
		return errors.WithMessage(err, "[repositories.GDTaskRepository] failed to marshal gd task")
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
		return errors.WithMessage(err, "failed to save gameap daemon task")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.GDTaskRepository] failed to save gameap daemon task",
		)
	}

	err = repository.serverRepository.Save(ctx, gdtask.Server())
	if err != nil {
		return errors.WithMessage(err, "failed to save game server")
	}

	return nil
}

func (repository *GDTaskRepository) AppendOutput(ctx context.Context, gdtask *domain.GDTask, output []byte) error {
	marshalled, err := json.Marshal(struct {
		Output string `json:"output"`
	}{string(output)})
	if err != nil {
		return errors.WithMessage(err, "[repositories.GDTaskRepository] failed to marshal output")
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
		return errors.WithMessage(err, "[repositories.GDTaskRepository] failed to save gameap daemon task")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.GDTaskRepository] failed to save gameap daemon task",
		)
	}

	return nil
}
