package definitions

import (
	"context"

	grpcclient "github.com/gameap/daemon/internal/app/grpc"
	"github.com/gameap/daemon/internal/app/repositories"
)

func CreateGameStore() *grpcclient.GameStore {
	return grpcclient.NewGameStore()
}

func CreateGatewayClient(ctx context.Context, c Container, gameStore *grpcclient.GameStore) *grpcclient.GatewayClient {
	cfg := c.Cfg(ctx)

	taskManager := c.Services().GdTaskManager(ctx)
	serverRepo := c.Repositories().ServerRepository(ctx).(*repositories.ServerRepository)

	taskHandler := grpcclient.NewGRPCTaskHandler(
		taskManager,
		serverRepo,
	)

	commandHandler := grpcclient.NewGRPCCommandHandler(
		c.Services().ExtendableExecutor(ctx),
		cfg.WorkPath,
	)

	fileHandler := grpcclient.NewGRPCFileHandler(cfg.WorkPath)

	serverHandler := grpcclient.NewGRPCServerHandler(
		serverRepo,
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
		taskManager,
		serverRepo,
	)

	return client
}

func CreateConnectionManager(ctx context.Context, c Container, gameStore *grpcclient.GameStore) *grpcclient.ConnectionManager {
	cfg := c.Cfg(ctx)
	fileTransferClient := CreateFileTransferClient(ctx, c)
	client := CreateGatewayClient(ctx, c, gameStore)

	transferHandler := grpcclient.NewGRPCTransferHandler(
		cfg.WorkPath,
		fileTransferClient,
		client,
		4,
	)
	client.SetTransferHandler(transferHandler)

	c.Services().GdTaskManager(ctx).SetTaskStatusSender(client)

	cm := grpcclient.NewConnectionManager(cfg, client)
	cm.OnConnect(fileTransferClient.SetConnection)

	return cm
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
