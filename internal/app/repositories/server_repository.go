package repositories

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/limiter"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const serverCacheTTL = 10 * time.Second

// limit scheduler consts
const (
	schedulerDefaultDuration        = 1 * time.Second
	schedulerDefaultBulkCallFromNum = 5
	schedulerDefaultBulkSize        = 100
)

type ServerRepository struct {
	innerRepo apiServerRepo

	limitScheduler *limiter.CallScheduler

	mu          sync.Mutex
	servers     sync.Map // [int]*domain.Server  (serverID => server)
	lastUpdated sync.Map // [int]time.Time		 (serverID => time)
}

func NewServerRepository(ctx context.Context, client contracts.APIRequestMaker, logger *log.Logger) *ServerRepository {
	serverRepo := &ServerRepository{
		innerRepo: apiServerRepo{
			client: client,
		},
	}

	limitScheduler := limiter.NewAPICallScheduler(
		schedulerDefaultDuration,
		schedulerDefaultBulkCallFromNum,
		func(ctx context.Context, q *limiter.Queue) error {
			server, ok := q.Get().(*domain.Server)
			if !ok {
				return errors.New("failed to get server from queue")
			}

			err := serverRepo.innerRepo.Save(ctx, server)
			if err != nil {
				return errors.WithMessage(err, "failed to save server")
			}

			return nil
		},
		func(ctx context.Context, q *limiter.Queue) error {
			s := q.GetN(schedulerDefaultBulkSize)
			servers := make([]*domain.Server, 0, len(s))
			for i := range s {
				server, ok := s[i].(*domain.Server)
				if !ok {
					return errors.New("failed to get server from queue")
				}

				servers = append(servers, server)
			}

			err := serverRepo.innerRepo.SaveBulk(ctx, servers)
			if err != nil {
				return errors.WithMessage(err, "failed to save servers")
			}

			return nil
		},
		logger,
	)

	go limitScheduler.Run(ctx)

	serverRepo.limitScheduler = limitScheduler

	return serverRepo
}

func (repo *ServerRepository) IDs(ctx context.Context) ([]int, error) {
	return repo.innerRepo.IDs(ctx)
}

func (repo *ServerRepository) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	var err error
	var server *domain.Server

	loadedServer, ok := repo.servers.Load(id)
	//nolint:nestif
	if !ok {
		server, err = repo.innerRepo.FindByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if server != nil {
			repo.lastUpdated.Store(id, time.Now())
		}
	} else {
		server = loadedServer.(*domain.Server)

		lastUpdated, ok := repo.lastUpdated.Load(id)
		if ok && time.Until(lastUpdated.(time.Time))+serverCacheTTL < 0 && !server.IsModified() {
			server, err = repo.innerRepo.FindByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if server != nil {
				repo.lastUpdated.Store(id, time.Now())
			}
		}
	}

	if server != nil {
		repo.servers.Store(id, server)
	}

	return server, nil
}

func (repo *ServerRepository) Save(ctx context.Context, server *domain.Server) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	dbServer, err := repo.innerRepo.FindByID(ctx, server.ID())
	if err != nil {
		return errors.WithMessage(err, "failed to find server before saving")
	}

	err = repo.innerRepo.Save(ctx, repo.resolveConflicts(server, dbServer))
	if err != nil {
		return err
	}

	server.UnmarkModifiedFlag()

	return nil
}

func (repo *ServerRepository) resolveConflicts(cacheServer, dbServer *domain.Server) *domain.Server {
	server := cacheServer

	if !cacheServer.IsValueModified("installationStatus") &&
		cacheServer.InstallationStatus() != dbServer.InstallationStatus() {
		server.SetInstallationStatus(dbServer.InstallationStatus())
	}

	return server
}

//nolint:maligned
type serverStruct struct {
	ID            int  `json:"id"`
	Enabled       bool `json:"enabled"`
	InstallStatus int  `json:"installed"`
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

type apiServerRepo struct {
	client contracts.APIRequestMaker

	servers sync.Map // [int]*domain.Server  (serverID => server)
}

func (apiRepo *apiServerRepo) IDs(ctx context.Context) ([]int, error) {
	response, err := apiRepo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "/gdaemon_api/servers",
	})

	if err != nil {
		return nil, err
	}

	if response.StatusCode() != http.StatusOK {
		return nil, domain.NewErrInvalidResponseFromAPI(response.StatusCode(), response.Body())
	}

	var srvList []struct {
		ID int `json:"id"`
	}
	err = json.Unmarshal(response.Body(), &srvList)
	if err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(srvList))

	for _, v := range srvList {
		ids = append(ids, v.ID)
	}

	return ids, nil
}

//nolint:funlen
func (apiRepo *apiServerRepo) FindByID(ctx context.Context, id int) (*domain.Server, error) {
	response, err := apiRepo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodGet,
		URL:    "/gdaemon_api/servers/{id}",
		PathParams: map[string]string{
			"id": strconv.Itoa(id),
		},
	})

	if err != nil {
		return nil, err
	}

	if response.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode() != http.StatusOK {
		return nil, errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(response.StatusCode(), response.Body()),
			"[repositories.apiServerRepo] failed find game server",
		)
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
			lastProcessCheck, err = time.Parse(time.RFC3339, srv.LastProcessCheck)
			if err != nil {
				return nil, err
			}
		}
	}

	updatedAt, err := time.Parse(time.RFC3339, srv.UpdatedAt)
	if err != nil {
		return nil, err
	}

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

	var server *domain.Server
	if item, exists := apiRepo.servers.Load(srv.ID); exists {
		server = item.(*domain.Server)
		server.Set(
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

	server = domain.NewServer(
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

	apiRepo.servers.Store(srv.ID, server)

	return server, nil
}

type serverSaveStruct struct {
	InstallationStatus uint8  `json:"installed"`
	ProcessActive      uint8  `json:"process_active"`
	LastProcessCheck   string `json:"last_process_check"`
}

func saveStructFromServer(server *domain.Server) serverSaveStruct {
	saveStruct := serverSaveStruct{
		ProcessActive: 0,
	}

	if server.IsValueModified("installationStatus") {
		saveStruct.InstallationStatus = uint8(server.InstallationStatus())
	}

	if server.IsActive() && server.IsValueModified("status") {
		saveStruct.ProcessActive = 1
	}

	if !server.LastStatusCheck().IsZero() && server.IsValueModified("status") {
		saveStruct.LastProcessCheck = server.LastStatusCheck().UTC().Format("2006-01-02 15:04:05")
	}

	return saveStruct
}

func (apiRepo *apiServerRepo) Save(ctx context.Context, server *domain.Server) error {
	serverSaveValues := saveStructFromServer(server)

	marshalled, err := json.Marshal(serverSaveValues)
	if err != nil {
		return errors.WithMessage(err, "[repositories.apiServerRepo] failed to marshal server")
	}

	resp, err := apiRepo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPut,
		URL:    "/gdaemon_api/servers/{id}",
		Body:   marshalled,
		PathParams: map[string]string{
			"id": strconv.Itoa(server.ID()),
		},
	})
	if err != nil {
		return errors.WithMessage(err, "[repositories.apiServerRepo] failed to saving server")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.apiServerRepo] failed to saving server",
		)
	}

	return nil
}

func (apiRepo *apiServerRepo) SaveBulk(ctx context.Context, servers []*domain.Server) error {
	serverSaveValues := make([]serverSaveStruct, 0, len(servers))
	for i := range servers {
		serverSaveValues = append(serverSaveValues, saveStructFromServer(servers[i]))
	}

	marshalled, err := json.Marshal(serverSaveValues)
	if err != nil {
		return errors.WithMessage(err, "[repositories.apiServerRepo] failed to marshal servers")
	}

	resp, err := apiRepo.client.Request(ctx, domain.APIRequest{
		Method: http.MethodPatch,
		URL:    "/gdaemon_api/servers",
		Body:   marshalled,
	})
	if err != nil {
		return errors.WithMessage(err, "[repositories.apiServerRepo] failed to bulk saving servers")
	}

	if resp.StatusCode() != http.StatusOK {
		return errors.WithMessage(
			domain.NewErrInvalidResponseFromAPI(resp.StatusCode(), resp.Body()),
			"[repositories.apiServerRepo] failed to bulk saving servers",
		)
	}

	return nil
}
