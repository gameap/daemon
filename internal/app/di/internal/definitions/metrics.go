package definitions

import (
	"context"

	grpcclient "github.com/gameap/daemon/internal/app/grpc"
	"github.com/gameap/daemon/internal/app/metrics"
	"github.com/gameap/daemon/internal/app/repositories"
)

func CreateMetricsService(ctx context.Context, c Container) *metrics.Service {
	cfg := c.Cfg(ctx)
	serverRepo := c.Repositories().ServerRepository(ctx).(*repositories.ServerRepository)
	pm := c.Services().ProcessManager(ctx)

	buffer := metrics.NewBuffer(cfg.Metrics.RetentionDuration)

	nodeCollector := metrics.NewNodeMetricsCollector(cfg)
	serversCollector := metrics.NewServersMetricsCollector(serverRepo, pm)

	return metrics.NewService(
		buffer,
		cfg.Metrics.CollectionInterval,
		nodeCollector,
		serversCollector,
	)
}

// AttachMetricsHandler wires the metrics service into the gateway client.
// Kept separate from CreateMetricsService so the metrics service can be
// constructed independently of the GatewayClient (e.g. for tests).
func AttachMetricsHandler(client *grpcclient.GatewayClient, service *metrics.Service) {
	handler := grpcclient.NewGRPCMetricsHandler(service)
	client.SetMetricsHandler(handler)
}
