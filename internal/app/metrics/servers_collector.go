package metrics

import (
	"context"
	"strconv"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	log "github.com/sirupsen/logrus"
)

const (
	labelServerID = "server_id"
)

// ServerLister exposes the cached set of servers known to the daemon.
// Implemented by *repositories.ServerRepository (IDsFromCache /
// FindByIDFromCache). Defined as a small interface here to keep the metrics
// package decoupled from repositories.
type ServerLister interface {
	IDsFromCache() []int
	FindByIDFromCache(id int) (*domain.Server, bool)
}

// ServersMetricsCollector iterates known servers and asks the underlying
// process manager for per-server metrics. Failures for individual servers
// are logged and skipped — one broken server must not stop the rest.
type ServersMetricsCollector struct {
	servers ServerLister
	pm      contracts.ProcessManager
}

func NewServersMetricsCollector(servers ServerLister, pm contracts.ProcessManager) *ServersMetricsCollector {
	return &ServersMetricsCollector{servers: servers, pm: pm}
}

func (c *ServersMetricsCollector) Collect(ctx context.Context) ([]domain.Metric, error) {
	ids := c.servers.IDsFromCache()
	out := make([]domain.Metric, 0, len(ids)*4)

	for _, id := range ids {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}

		server, ok := c.servers.FindByIDFromCache(id)
		if !ok || server == nil {
			continue
		}

		metrics, err := c.pm.Metrics(ctx, server)
		if err != nil {
			log.WithError(err).
				WithField("server_id", server.ID()).
				Warn("collect server metrics failed")
			continue
		}

		serverIDLabel := strconv.Itoa(server.ID())
		for i := range metrics {
			ensureServerLabels(&metrics[i], serverIDLabel, server.UUID())
		}

		out = append(out, metrics...)
	}

	return out, nil
}

// ensureServerLabels guarantees server_id is set so the panel can correlate
// per-server samples; if the process manager already set it (e.g. with a
// container-specific value), we leave its choice in place.
func ensureServerLabels(m *domain.Metric, serverID, serverUUID string) {
	if m.Labels == nil {
		m.Labels = make(map[string]string, 2)
	}
	if _, ok := m.Labels[labelServerID]; !ok {
		m.Labels[labelServerID] = serverID
	}
	if _, ok := m.Labels["server_uuid"]; !ok && serverUUID != "" {
		m.Labels["server_uuid"] = serverUUID
	}
}
