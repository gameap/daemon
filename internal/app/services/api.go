package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/go-resty/resty/v2"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	lock "github.com/viney-shih/go-lock"
)

const maxActualizeCount = 1

var (
	errInvalidRequestMethod         = errors.New("invalid request method")
	errActualizeTokenActionIsLocked = errors.New("actualize token action is already locked")
)

type APIClient struct {
	innerClient *resty.Client
	cfg         *config.Config

	// runtime
	tokenMutex    *lock.CASMutex
	apiServerTime time.Time
	token         string
}

func NewAPICaller(ctx context.Context, cfg *config.Config, client *resty.Client) (*APIClient, error) {
	api := &APIClient{
		innerClient: client,
		cfg:         cfg,
		tokenMutex:  lock.NewCASMutex(),
	}

	err := api.actualizeToken(ctx)

	return api, err
}

func (c *APIClient) Request(ctx context.Context, request domain.APIRequest) (contracts.APIResponse, error) {
	return c.request(ctx, request, 0)
}

//nolint:funlen
func (c *APIClient) request(
	ctx context.Context,
	request domain.APIRequest,
	deep uint8,
) (contracts.APIResponse, error) {
	restyRequest := c.innerClient.R()

	restyRequest.SetHeader("Content-Type", "application/json")
	restyRequest.SetHeader("X-Auth-Token", c.token)

	if len(request.QueryParams) > 0 {
		restyRequest.SetQueryParams(request.QueryParams)
	}

	if len(request.PathParams) > 0 {
		restyRequest.SetPathParams(request.PathParams)
	}

	if len(request.Header) > 0 {
		for key, values := range request.Header {
			for _, v := range values {
				restyRequest.SetHeader(key, v)
			}
		}
	}

	if len(request.Body) > 0 {
		restyRequest.SetBody(request.Body)
	}

	restyRequest.SetContext(ctx)

	var err error
	var response *resty.Response

	l := logger.Logger(ctx)

	switch request.Method {
	case http.MethodGet:
		response, err = restyRequest.Get(request.URL)
	case http.MethodPost:
		body, isBytes := restyRequest.Body.([]byte)
		if isBytes {
			l = l.WithField("body", string(body))
		}

		response, err = restyRequest.Post(request.URL)
	case http.MethodPut:
		body, isBytes := restyRequest.Body.([]byte)
		if isBytes {
			l = l.WithField("body", string(body))
		}

		response, err = restyRequest.Put(request.URL)
	default:
		return nil, errInvalidRequestMethod
	}

	l.WithFields(logrus.Fields{
		"requestURL":     restyRequest.URL,
		"responseStatus": response.StatusCode(),
	}).Debug("api request")

	if err != nil {
		return nil, err
	}

	if response.StatusCode() == http.StatusUnauthorized && deep < maxActualizeCount {
		logger.Warn(ctx, "invalid token, actualizing token")
		err = c.actualizeToken(ctx)
		if err != nil {
			return nil, err
		}
		return c.request(ctx, request, deep+1)
	}

	return response, nil
}

func (c *APIClient) actualizeToken(ctx context.Context) error {
	locked := c.tokenMutex.TryLockWithContext(ctx)
	if !locked {
		return errActualizeTokenActionIsLocked
	}
	defer c.tokenMutex.Unlock()

	request := c.innerClient.R()

	request.SetContext(ctx)

	request.SetHeader("Content-Type", "application/json")
	request.SetHeader("Authorization", fmt.Sprintf("Bearer %s", c.cfg.APIKey))

	response, err := request.Get("/gdaemon_api/get_token")
	if err != nil {
		return errors.WithMessage(err, "failed to get gdaemon API token")
	}

	if response.IsError() {
		return domain.NewErrInvalidResponseFromAPI(response.StatusCode(), response.Body())
	}

	message := struct {
		Token     string `json:"token"`
		Timestamp int64  `json:"timestamp"`
	}{}

	err = json.Unmarshal(response.Body(), &message)
	if err != nil {
		return errors.WithMessage(err, "failed to unmarshal API response")
	}

	c.token = message.Token
	c.apiServerTime = time.Unix(message.Timestamp, 0)

	return nil
}
