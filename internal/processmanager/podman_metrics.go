//go:build linux || darwin

package processmanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

// podmanStatsResponse mirrors the JSON envelope returned by libpod's
// /containers/{name}/stats?stream=false endpoint. Field names match the
// libpod stats schema (ContainerStats); fields the daemon does not consume
// are omitted.
type podmanStatsResponse struct {
	Error string             `json:"Error,omitempty"`
	Stats []podmanStatsEntry `json:"Stats"`
}

type podmanStatsEntry struct {
	ContainerID string  `json:"ContainerID"`
	Name        string  `json:"Name"`
	PIDs        uint64  `json:"PIDs"`
	CPU         float64 `json:"CPU"`
	MemUsage    uint64  `json:"MemUsage"`
	MemLimit    uint64  `json:"MemLimit"`
	MemPerc     float64 `json:"MemPerc"`
	NetInput    uint64  `json:"NetInput"`
	NetOutput   uint64  `json:"NetOutput"`
	BlockInput  uint64  `json:"BlockInput"`
	BlockOutput uint64  `json:"BlockOutput"`
}

// Metrics returns CPU, memory, network and block-IO metrics for the
// container backing this server. Falls back to the cached liveness gauge
// alone when the container is missing or stats cannot be read.
func (pm *Podman) Metrics(ctx context.Context, server *domain.Server) ([]domain.Metric, error) {
	now := time.Now()
	out := make([]domain.Metric, 0, 12)
	out = append(out, livenessMetric(server, now))

	containerName := pm.resolveContainerName(ctx, server)

	path := "/containers/" + url.PathEscape(containerName) + "/stats?stream=false"
	resp, err := pm.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return out, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return out, errors.Wrapf(errPodmanInspectContainer,
			"podman stats returned %d: %s", resp.StatusCode, string(body))
	}

	var stats podmanStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return out, errors.Wrap(err, "failed to decode podman stats")
	}
	if stats.Error != "" {
		return out, errors.Errorf("podman stats reported error: %s", stats.Error)
	}
	if len(stats.Stats) == 0 {
		return out, nil
	}

	out = append(out, podmanStatsToMetrics(now, containerName, &stats.Stats[0])...)
	return out, nil
}

func podmanStatsToMetrics(ts time.Time, containerName string, s *podmanStatsEntry) []domain.Metric {
	labels := map[string]string{metricLabelContainer: containerName}
	out := make([]domain.Metric, 0, 8)

	out = append(out, domain.Metric{
		Name:      metricServerCPUUsagePercent,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitPercent,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Float64Value(s.CPU),
	})

	if s.MemUsage > 0 || s.MemLimit > 0 {
		out = append(out, domain.Metric{
			Name:      metricServerMemoryUsageBytes,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(s.MemUsage),
		})
		if s.MemLimit > 0 {
			out = append(out,
				domain.Metric{
					Name:      metricServerMemoryLimitBytes,
					Type:      domain.MetricTypeGauge,
					Unit:      domain.MetricUnitBytes,
					Labels:    cloneLabelMap(labels),
					Timestamp: ts,
					Value:     domain.Uint64Value(s.MemLimit),
				},
				domain.Metric{
					Name:      metricServerMemoryUsagePercent,
					Type:      domain.MetricTypeGauge,
					Unit:      domain.MetricUnitPercent,
					Labels:    cloneLabelMap(labels),
					Timestamp: ts,
					Value:     domain.Float64Value(s.MemPerc),
				},
			)
		}
	}

	out = append(out,
		domain.Metric{
			Name:      metricServerNetworkReceiveBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(s.NetInput),
		},
		domain.Metric{
			Name:      metricServerNetworkTransmitBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(s.NetOutput),
		},
		domain.Metric{
			Name:      metricServerBlockIOReadBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(s.BlockInput),
		},
		domain.Metric{
			Name:      metricServerBlockIOWriteBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(s.BlockOutput),
		},
	)

	out = append(out, domain.Metric{
		Name:      metricServerProcessPIDs,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitCount,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Uint64Value(s.PIDs),
	})

	return out
}
