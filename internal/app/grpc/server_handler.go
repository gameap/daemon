package grpc

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
)

type ServerCacheRepository interface {
	FindByIDFromCache(id int) (*domain.Server, bool)
	SaveToCache(server *domain.Server)
}

type GRPCServerHandler struct {
	serverRepo ServerCacheRepository
	gameStore  *GameStore
}

func NewGRPCServerHandler(serverRepo ServerCacheRepository, gameStore *GameStore) *GRPCServerHandler {
	return &GRPCServerHandler{
		serverRepo: serverRepo,
		gameStore:  gameStore,
	}
}

func (h *GRPCServerHandler) HandleServerUpdate(_ context.Context, srv *pb.Server) error {
	return h.handleServerProto(srv, nil)
}

func (h *GRPCServerHandler) HandleServerConfigUpdate(
	_ context.Context, srv *pb.Server, settings []*pb.ServerSetting,
) error {
	return h.handleServerProto(srv, parseProtoSettings(settings))
}

func (h *GRPCServerHandler) handleServerProto(srv *pb.Server, settings domain.Settings) error {
	serverID := int(srv.Id)

	var lastProcessCheck time.Time
	if srv.LastProcessCheck != nil {
		lastProcessCheck = srv.GetLastProcessCheck().AsTime()
	}

	var updatedAt time.Time
	if srv.UpdatedAt != nil {
		updatedAt = srv.GetUpdatedAt().AsTime()
	}

	game, gameFound := h.gameStore.FindGame(srv.GameId)
	gameMod, gameModFound := h.gameStore.FindGameMod(srv.GameModId)

	existing, found := h.serverRepo.FindByIDFromCache(serverID)
	if found {
		if !gameFound {
			game = existing.Game()
		}
		if !gameModFound {
			gameMod = existing.GameMod()
		}

		// Preserve existing settings when not explicitly provided
		// (backward compat with old server_config messages that don't include settings)
		if settings == nil {
			settings = existing.AllSettings()
		}

		existing.Set(
			srv.Enabled,
			ProtoInstalledStatusToDomain(srv.Installed),
			srv.Blocked,
			srv.Name,
			srv.Uuid,
			srv.UuidShort,
			game,
			gameMod,
			srv.ServerIp,
			int(srv.ServerPort),
			int(srv.GetQueryPort()),
			int(srv.GetRconPort()),
			srv.GetRcon(),
			srv.Dir,
			srv.GetSuUser(),
			srv.GetStartCommand(),
			srv.GetStopCommand(),
			srv.GetForceStopCommand(),
			srv.GetRestartCommand(),
			srv.ProcessActive,
			lastProcessCheck,
			parseVarsJSON(srv.GetVars()),
			settings,
			updatedAt,
			int(srv.GetCpuLimit()),
			int64(srv.GetRamLimit()),
		)

		h.serverRepo.SaveToCache(existing)
	} else {
		server := domain.NewServer(
			serverID,
			srv.Enabled,
			ProtoInstalledStatusToDomain(srv.Installed),
			srv.Blocked,
			srv.Name,
			srv.Uuid,
			srv.UuidShort,
			game,
			gameMod,
			srv.ServerIp,
			int(srv.ServerPort),
			int(srv.GetQueryPort()),
			int(srv.GetRconPort()),
			srv.GetRcon(),
			srv.Dir,
			srv.GetSuUser(),
			srv.GetStartCommand(),
			srv.GetStopCommand(),
			srv.GetForceStopCommand(),
			srv.GetRestartCommand(),
			srv.ProcessActive,
			lastProcessCheck,
			parseVarsJSON(srv.GetVars()),
			settings,
			updatedAt,
			int(srv.GetCpuLimit()),
			int64(srv.GetRamLimit()),
		)

		h.serverRepo.SaveToCache(server)
	}

	log.WithField("serverID", serverID).Info("Server updated")

	return nil
}
