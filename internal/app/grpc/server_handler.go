package grpc

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type GRPCServerHandler struct {
	serverRepository domain.ServerRepository
	gameStore        *GameStore
}

func NewGRPCServerHandler(serverRepository domain.ServerRepository, gameStore *GameStore) *GRPCServerHandler {
	return &GRPCServerHandler{
		serverRepository: serverRepository,
		gameStore:        gameStore,
	}
}

func (h *GRPCServerHandler) HandleServerUpdate(ctx context.Context, srv *pb.Server) error {
	serverID := int(srv.Id)

	server, err := h.serverRepository.FindByID(ctx, serverID)
	if err != nil {
		return errors.Wrapf(err, "failed to find server %d", serverID)
	}

	var lastProcessCheck time.Time
	if srv.LastProcessCheck != nil {
		lastProcessCheck = time.Unix(srv.GetLastProcessCheck(), 0)
	}

	var updatedAt time.Time
	if srv.UpdatedAt != nil {
		updatedAt = time.Unix(srv.GetUpdatedAt(), 0)
	}

	game := server.Game()
	if g, ok := h.gameStore.FindGame(srv.GameId); ok {
		game = g
	}

	gameMod := server.GameMod()
	if m, ok := h.gameStore.FindGameMod(srv.GameModId); ok {
		gameMod = m
	}

	server.Set(
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
		nil,
		updatedAt,
		int(srv.GetCpuLimit()),
		int64(srv.GetRamLimit()),
	)

	if err := h.serverRepository.Save(ctx, server); err != nil {
		return errors.Wrapf(err, "failed to save server %d", serverID)
	}

	log.WithField("serverID", serverID).Info("Server updated")

	return nil
}
