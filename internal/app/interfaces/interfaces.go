package interfaces

import (
	"context"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

type Cache interface {
	Set(ctx context.Context, key string, val interface{}, ttl time.Duration)
	Get(ctx context.Context, key string)
	Delete(ctx context.Context, key string)
}

type Store interface {
	Set(ctx context.Context, key string, val interface{})
	Get(ctx context.Context, key string)
	Delete(ctx context.Context, key string)
}

type OutputReader interface {
	ReadOutput() []byte
}

type Command interface {
	OutputReader

	Execute(ctx context.Context, server *domain.Server) error
	Result() int
	IsComplete() bool
}
