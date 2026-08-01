package internal

import (
	"context"
	"testing"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func newTestContainer() *Container {
	cfg := &config.Config{}
	cfg.ProcessManager.Name = "simple"

	c := NewContainer()
	c.SetCfg(cfg)
	c.SetLogger(logrus.New())

	return c
}

// Regression test: resolving ServerCommandFactory first (as CreateProcessRunner does)
// caches the clean executor; ExtendableExecutor must not return it, otherwise
// custom handlers like "get-tool" are lost and commands fall through to exec.LookPath.
func TestExtendableExecutor_AfterServerCommandFactoryResolved_ExpectExtendableExecutor(t *testing.T) {
	c := newTestContainer()
	ctx := context.Background()

	factory := c.ServerCommandFactory(ctx)
	extendable := c.Services().ExtendableExecutor(ctx)

	require.NoError(t, c.Error())
	require.NotNil(t, factory)
	require.IsType(t, &components.ExtendableExecutor{}, extendable)
}

func TestExecutors_ResolveExecutorFirst_ExpectDistinctExecutors(t *testing.T) {
	c := newTestContainer()
	ctx := context.Background()

	executor := c.Services().Executor(ctx)
	extendable := c.Services().ExtendableExecutor(ctx)

	require.NoError(t, c.Error())
	require.IsType(t, &components.Executor{}, executor)
	require.IsType(t, &components.ExtendableExecutor{}, extendable)
}

func TestExecutors_ResolveExtendableExecutorFirst_ExpectDistinctExecutors(t *testing.T) {
	c := newTestContainer()
	ctx := context.Background()

	extendable := c.Services().ExtendableExecutor(ctx)
	executor := c.Services().Executor(ctx)

	require.NoError(t, c.Error())
	require.IsType(t, &components.ExtendableExecutor{}, extendable)
	require.IsType(t, &components.Executor{}, executor)
}
