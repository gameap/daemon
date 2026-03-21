package definitions

import (
	"context"

	grpcclient "github.com/gameap/daemon/internal/app/grpc"
)

func CreateGameStore() *grpcclient.GameStore {
	return grpcclient.NewGameStore()
}

func CreateGatewayClient(ctx context.Context, c Container, gameStore *grpcclient.GameStore) *grpcclient.GatewayClient {
	cfg := c.Cfg(ctx)

	taskHandler := grpcclient.NewGRPCTaskHandler(
		c.Services().GdTaskManager(ctx),
		c.Repositories().ServerRepository(ctx),
	)

	commandHandler := grpcclient.NewGRPCCommandHandler(
		c.Services().ExtendableExecutor(ctx),
		cfg.WorkPath,
	)

	fileHandler := grpcclient.NewGRPCFileHandler(cfg.WorkPath)

	serverHandler := grpcclient.NewGRPCServerHandler(
		c.Repositories().ServerRepository(ctx),
		gameStore,
	)

	heartbeatCollector := grpcclient.NewHeartbeatCollector(cfg.WorkPath)

	client := grpcclient.NewGatewayClient(
		cfg,
		taskHandler,
		commandHandler,
		fileHandler,
		serverHandler,
		heartbeatCollector,
		nil,
		taskHandler,
		gameStore,
	)

	return client
}

func CreateConnectionManager(ctx context.Context, c Container, gameStore *grpcclient.GameStore) *grpcclient.ConnectionManager {
	cfg := c.Cfg(ctx)
	client := CreateGatewayClient(ctx, c, gameStore)

	return grpcclient.NewConnectionManager(cfg, client)
}

func CreateFileTransferClient(ctx context.Context, c Container) *grpcclient.FileTransferClient {
	cfg := c.Cfg(ctx)
	return grpcclient.NewFileTransferClient(cfg)
}

func CreateServerStatusReporter(
	_ context.Context, _ Container, client *grpcclient.GatewayClient,
) *grpcclient.ServerStatusReporter {
	return grpcclient.NewServerStatusReporter(client)
}
