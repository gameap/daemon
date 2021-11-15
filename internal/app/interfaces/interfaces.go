package interfaces

import (
	"context"
	"io"
	"time"

	"github.com/gameap/daemon/internal/app/components"
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

type APIRequestMaker interface {
	Request(ctx context.Context, request domain.APIRequest) (APIResponse, error)
}

type APIResponse interface {
	Body() []byte
	Status() string
	StatusCode() int

	Error() interface{}
}

type Executor interface {
	Exec(ctx context.Context, command string, options components.ExecutorOptions) ([]byte, int, error)
	ExecWithWriter(ctx context.Context, command string, out io.Writer, options components.ExecutorOptions) (int, error)
}

type GameProcessController interface {
	Start(ctx context.Context, server *domain.Server) (int, error)
	Stop(ctx context.Context, server *domain.Server) (int, error)
	Restart(ctx context.Context, server *domain.Server) (int, error)
	Status(ctx context.Context, server *domain.Server) (int, error)
	GetOutput(ctx context.Context, server *domain.Server) (int, error)
	SendInput(ctx context.Context, server *domain.Server) (int, error)
}
