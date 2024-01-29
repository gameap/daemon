package definitions

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/repositories"
)

func CreateRepositoriesGdTaskRepository(ctx context.Context, c Container) domain.GDTaskRepository {
	return repositories.NewGDTaskRepository(
		c.Services().ApiCaller(ctx),
		c.Repositories().ServerRepository(ctx),
	)
}

func CreateRepositoriesServerRepository(ctx context.Context, c Container) domain.ServerRepository {
	return repositories.NewServerRepository(ctx, c.Services().ApiCaller(ctx), c.Logger(ctx))
}

func CreateRepositoriesServerTaskRepository(ctx context.Context, c Container) domain.ServerTaskRepository {
	return repositories.NewServerTaskRepository(c.Services().ApiCaller(ctx), c.Repositories().ServerRepository(ctx))
}
