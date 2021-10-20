package game_server_commands

import (
	"context"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
)

type reinstallServer struct {
	cfg *config.Config
}

func newReinstallServer(cfg *config.Config) *reinstallServer {
	return &reinstallServer{cfg}
}

func (s *reinstallServer) Execute(ctx context.Context, server *domain.Server) error {
	panic("implement me")
}

func (s *reinstallServer) Result() int {
	panic("implement me")
}

func (s *reinstallServer) IsComplete() bool {
	panic("implement me")
}

func (s *reinstallServer) ReadOutput() []byte {
	panic("implement me")
}
