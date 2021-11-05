package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/interfaces"
	"github.com/go-resty/resty/v2"
	"github.com/pkg/errors"
)

var invalidAPIResponse = errors.New("failed to get gdaemon API token")

type APIClient struct {
	innerClient *resty.Client
	cfg         *config.Config

	// runtime
	apiServerTime time.Time
	token         string
}

type innerClient interface {
	SetHeaders(headers map[string]string) *innerClient
}

func NewAPICaller(ctx context.Context, cfg *config.Config, client *resty.Client) (*APIClient, error) {
	api := &APIClient{
		innerClient: client,
		cfg: cfg,
	}

	err := api.sync(ctx)

	return api, err
}

func (c *APIClient) Request() interfaces.APIRequest {
	request := c.innerClient.R()

	request.SetHeader("X-Auth-Token", c.token)
	request.SetHeader("Content-Type", "application/json")

	return newRequest(request)
}

func (c *APIClient) sync(ctx context.Context) error {
	request := c.innerClient.R()

	request.SetContext(ctx)

	request.SetHeader("Content-Type", "application/json")
	request.SetHeader("Authorization", fmt.Sprintf("Bearer %s", c.cfg.APIKey))

	response, err := request.Get("/gdaemon_api/get_token")
	if err != nil {
		return errors.WithMessage(err, "failed to get gdaemon API token")
	}

	if response.IsError() {
		return invalidAPIResponse
	}

	message := struct {
		Token     string `json:"token"`
		Timestamp int64	 `json:"timestamp"`
	}{}

	err = json.Unmarshal(response.Body(), &message)
	if err != nil {
		return errors.WithMessage(err, "failed to unmarshal API response")
	}

	c.token = message.Token
	c.apiServerTime = time.Unix(message.Timestamp, 0)

	return nil
}

type APIRequest struct {
	request *resty.Request
}

func newRequest(request *resty.Request) interfaces.APIRequest {
	return &APIRequest{request: request}
}

func (r *APIRequest) SetContext(ctx context.Context) interfaces.APIRequest {
	r.request = r.request.SetContext(ctx)
	return r
}

func (r *APIRequest) SetHeader(header, value string) interfaces.APIRequest {
	r.request = r.request.SetHeader(header, value)
	return r
}

func (r *APIRequest) SetHeaders(headers map[string]string) interfaces.APIRequest {
	r.request = r.request.SetHeaders(headers)
	return r
}

func (r *APIRequest) SetQueryParams(params map[string]string) interfaces.APIRequest {
	r.request = r.request.SetQueryParams(params)
	return r
}

func (r *APIRequest) SetPathParams(params map[string]string) interfaces.APIRequest {
	r.request = r.request.SetPathParams(params)
	return r
}

func (r *APIRequest) Get(url string) (interfaces.APIResponse, error) {
	return r.request.Get(url)
}
