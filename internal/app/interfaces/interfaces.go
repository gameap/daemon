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

type APIRequestMaker interface {
	Request() APIRequest
}

type APIRequest interface {
	SetContext(ctx context.Context) APIRequest
	SetHeader(header, value string) APIRequest
	SetHeaders(headers map[string]string) APIRequest
	SetPathParams(params map[string]string) APIRequest
	SetQueryParams(params map[string]string) APIRequest

	Get(url string) (APIResponse, error)
}

type APIResponse interface {
	Body() []byte
	Status() string
	StatusCode() int

	Error() interface{}
}
