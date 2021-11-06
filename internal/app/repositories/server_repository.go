package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

const updateTimeout = 5 * time.Minute

var invalidResponseFromAPIServer = errors.New("invalid response from api server")

type ServerRepository struct {
	innerRepo   apiRepo

	mu          sync.Mutex
	servers     sync.Map // [int]*domain.Server  (serverID => server)
	lastUpdated sync.Map // [int]time.Time		 (serverID => time)
}

func NewServerRepository(client interfaces.APIRequestMaker) *ServerRepository {
	return &ServerRepository{
		innerRepo: apiRepo{
			client: client,
		},
	}
}

func (repo *ServerRepository) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	var err error
	var server *domain.Server

	loadedServer, ok := repo.servers.Load(id)
	if !ok {
		server, err = repo.innerRepo.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
	} else {
		server = loadedServer.(*domain.Server)
	}

	lastUpdated, ok := repo.lastUpdated.Load(id)

	if ok && lastUpdated.(time.Time).Sub(time.Now()) > updateTimeout {
		server, err = repo.innerRepo.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
	}

	repo.lastUpdated.Store(id, time.Now())
	repo.servers.Store(id, server)

	return server, nil
}

func (repo *ServerRepository) Save(ctx context.Context, task *domain.Server) error {
	panic("implement me")
}


type serverStruct struct {
	ID            int  `json:"id"`
	Enabled       bool `json:"enabled"`
	InstallStatus int  `json:"install_status"`
	Blocked       bool `json:"blocked"`

	Name      string `json:"name"`
	UUID      string `json:"uuid"`
	UUIDShort string `json:"uuid_short"`

	Game    domain.Game    `json:"game"`
	GameMod domain.GameMod `json:"game_mod"`

	IP           string `json:"server_ip"`
	ConnectPort  int    `json:"server_port"`
	QueryPort    int    `json:"query_port"`
	RconPort     int    `json:"rcon_port"`
	RconPassword string `json:"rcon"`

	Dir  string `json:"dir"`
	User string `json:"su_user"`

	StartCommand     string `json:"start_command"`
	StopCommand      string `json:"stop_command"`
	ForceStopCommand string `json:"force_stop_command"`
	RestartCommand   string `json:"restart_command"`

	ProcessActive    bool   `json:"process_active"`
	LastProcessCheck string `json:"last_process_check"`

	Vars map[string]string `json:"vars"`

	Settings []map[string]interface{} `json:"settings"`

	UpdatedAt string `json:"updated_at"`
}

type apiRepo struct {
	client interfaces.APIRequestMaker
}

func (apiRepo *apiRepo) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	response, err := apiRepo.client.Request().
		SetContext(ctx).
		SetPathParams(map[string]string{
			"id": strconv.Itoa(id),
		}).
		Get("/gdaemon_api/servers/{id}")

	if err != nil {
		return nil, err
	}

	if response.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode() != http.StatusOK {
		return nil, invalidResponseFromAPIServer
	}

	var srv serverStruct
	err = json.Unmarshal(response.Body(), &srv)
	if err != nil {
		return nil, err
	}

	var lastProcessCheck time.Time
	if srv.LastProcessCheck != "" {
		lastProcessCheck, err = time.Parse("2006-01-02 15:04:05", srv.LastProcessCheck)
		if err != nil {
			return nil, err
		}
	}

	updatedAt, err := time.Parse(time.RFC3339, srv.UpdatedAt)

	settings := domain.Settings{}

	for _, v := range srv.Settings {
		sname, ok := v["name"]
		if !ok {
			continue
		}

		snameString, ok := sname.(string)
		if !ok {
			continue
		}

		svalue, ok := v["value"]
		if !ok {
			continue
		}

		svalueString, ok := svalue.(string)
		if !ok {
			continue
		}

		settings[snameString] = svalueString
	}

	server := domain.NewServer(
		srv.ID,
		srv.Enabled,
		domain.InstallationStatus(srv.InstallStatus),
		srv.Blocked,
		srv.Name,
		srv.UUID,
		srv.UUIDShort,
		srv.Game,
		srv.GameMod,
		srv.IP,
		srv.ConnectPort,
		srv.QueryPort,
		srv.RconPort,
		srv.RconPassword,
		srv.Dir,
		srv.User,
		srv.StartCommand,
		srv.StopCommand,
		srv.ForceStopCommand,
		srv.RestartCommand,
		srv.ProcessActive,
		lastProcessCheck,
		srv.Vars,
		settings,
		updatedAt,
	)

	return server, nil
}
