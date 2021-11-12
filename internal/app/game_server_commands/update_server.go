package game_server_commands

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type updateServer struct {
	cfg *config.Config
}

func newUpdateServer(cfg *config.Config, executor interfaces.Executor) *updateServer {
	return &updateServer{cfg}
}

func (s *updateServer) Execute(ctx context.Context, server *domain.Server) error {
	panic("implement me")
}

func (s *updateServer) Result() int {
	panic("implement me")
}

func (s *updateServer) IsComplete() bool {
	panic("implement me")
}

func (s *updateServer) ReadOutput() []byte {
	panic("implement me")
}
