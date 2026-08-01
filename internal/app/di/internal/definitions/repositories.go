package definitions

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/repositories"
)

func CreateRepositoriesServerRepository(_ context.Context, _ Container) domain.ServerRepository {
	return repositories.NewServerRepository()
}
