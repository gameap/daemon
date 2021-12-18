package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type ServerTaskRepository struct {
	client           contracts.APIRequestMaker
	serverRepository domain.ServerRepository
}

func NewServerTaskRepository(
	client contracts.APIRequestMaker,
	serverRepository domain.ServerRepository,
) *ServerTaskRepository {
	return &ServerTaskRepository{
		client:           client,
		serverRepository: serverRepository,
	}
}

type serverTask struct {
	ID           int    `json:"id"`
	Command      string `json:"command"`
	ServerID     int    `json:"server_id"`
	Repeat       int    `json:"repeat"`
	RepeatPeriod int    `json:"repeat_period"`
	Counter      int    `json:"counter"`
	ExecuteDate  string `json:"execute_date"`
}

func (repo *ServerTaskRepository) Find(ctx context.Context) ([]*domain.ServerTask, error) {
	resp, err := repo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "/gdaemon_api/servers_tasks",
	})

	if err != nil {
		return nil, errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to find game server tasks")
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.ServerTaskRepository] failed to find game servers tasks",
		)
	}

	var items []serverTask
	err = json.Unmarshal(resp.Body(), &items)
	if err != nil {
		return nil, errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to unmarshal server tasks")
	}

	tasks := make([]*domain.ServerTask, 0, len(items))
	for i := range items {
		server, err := repo.serverRepository.FindByID(ctx, items[i].ServerID)
		if err != nil {
			return nil, errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to join server to server task")
		}
		if server == nil {
			return nil, errInvalidServerID
		}

		executeDate, err := time.Parse("2006-01-02 15:04:05", items[i].ExecuteDate)
		if err != nil {
			return nil, errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to parse server task execute date")
		}

		task := domain.NewServerTask(
			items[i].ID,
			domain.ServerTaskCommand(items[i].Command),
			server,
			items[i].Repeat,
			time.Duration(items[i].RepeatPeriod)*time.Second,
			items[i].Counter,
			executeDate,
		)

		tasks = append(tasks, task)
	}

	return tasks, nil
}

func (repo *ServerTaskRepository) Save(ctx context.Context, task *domain.ServerTask) error {
	marshalled, err := json.Marshal(task)
	if err != nil {
		return errors.WithMessage(err, "failed to marshal server task")
	}

	resp, err := repo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPut,
		URL:    "/gdaemon_api/servers_tasks/{id}",
		Body:   marshalled,
		PathParams: map[string]string{
			"id": strconv.Itoa(task.ID()),
		},
	})
	if err != nil {
		return errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to save server task")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.ServerTaskRepository] failed to save server task",
		)
	}

	return nil
}

func (repo *ServerTaskRepository) Fail(ctx context.Context, task *domain.ServerTask, output []byte) error {
	marshalled, err := json.Marshal(struct {
		Output string `json:"output"`
	}{
		Output: string(output),
	})
	if err != nil {
		return errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to marshal server task output")
	}

	resp, err := repo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPost,
		URL:    "/gdaemon_api/servers_tasks/{id}/fail",
		Body:   marshalled,
		PathParams: map[string]string{
			"id": strconv.Itoa(task.ID()),
		},
	})
	if err != nil {
		return errors.WithMessage(err, "[repositories.ServerTaskRepository] failed to save server task fail info")
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.ServerTaskRepository] failed to save server task fail info",
		)
	}

	return nil
}
