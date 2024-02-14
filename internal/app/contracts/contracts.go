package contracts

import (
	"context"
	"io"
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

type CommandResultReader interface {
	OutputReader
	Result() int
	IsComplete() bool
}

type GameServerCommand interface {
	CommandResultReader

	Execute(ctx context.Context, server *domain.Server) error
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
	Exec(ctx context.Context, command string, options ExecutorOptions) ([]byte, int, error)
	ExecWithWriter(ctx context.Context, command string, out io.Writer, options ExecutorOptions) (int, error)
}

type ProcessManager interface {
	Start(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
	Stop(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
	Restart(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
	Status(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
	GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
	SendInput(ctx context.Context, input string, server *domain.Server, out io.Writer) (domain.Result, error)
}

type DomainPrimitiveValidator interface {
	Validate() error
}

type ExecutorOptions struct {
	WorkDir         string
	FallbackWorkDir string
	UID             string
	GID             string
	Env             map[string]string
}
