// Code generated by DIGEN; DO NOT EDIT.
// This file was generated by Dependency Injection Container Generator 0.0.3 (built at 2021-10-18T18:27:06Z).
// See docs at https://github.com/strider2038/digen

package di

import (
	"context"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/di/internal"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/services"
	"github.com/sirupsen/logrus"
	"sync"
)

type Container struct {
	mu *sync.Mutex
	c  *internal.Container
}

type Injector func(c *Container) error

func NewContainer(
	cfg *config.Config,
	logger *logrus.Logger,
	injectors ...Injector,
) (*Container, error) {
	c := &Container{
		mu: &sync.Mutex{},
		c:  internal.NewContainer(),
	}

	c.c.SetCfg(cfg)
	c.c.SetLogger(logger)

	for _, inject := range injectors {
		err := inject(c)
		if err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (c *Container) ProcessRunner(ctx context.Context) (*services.Runner, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := c.c.ProcessRunner(ctx)
	err := c.c.Error()
	if err != nil {
		return nil, err
	}

	return s, err
}

func SetApiCaller(s contracts.APIRequestMaker) Injector {
	return func(c *Container) error {
		c.c.Services().(*internal.ServicesContainer).SetAPICaller(s)

		return nil
	}
}

func (c *Container) GdTaskRepository(ctx context.Context) (domain.GDTaskRepository, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := c.c.Repositories().(*internal.RepositoryContainer).GdTaskRepository(ctx)
	err := c.c.Error()
	if err != nil {
		return nil, err
	}

	return s, err
}

func (c *Container) ServerRepository(ctx context.Context) (domain.ServerRepository, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := c.c.Repositories().(*internal.RepositoryContainer).ServerRepository(ctx)
	err := c.c.Error()
	if err != nil {
		return nil, err
	}

	return s, err
}

func (c *Container) ServerTaskRepository(ctx context.Context) (domain.ServerTaskRepository, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	s := c.c.Repositories().(*internal.RepositoryContainer).ServerTaskRepository(ctx)
	err := c.c.Error()
	if err != nil {
		return nil, err
	}

	return s, err
}

func (c *Container) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.c.Close()
}
