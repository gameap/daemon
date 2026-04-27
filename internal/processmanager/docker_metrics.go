//go:build linux || darwin || windows

package processmanager

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

// Metrics returns CPU, memory, network and block-IO metrics for the
// container backing this server. Falls back to the cached liveness gauge
// alone when the container is missing or stats cannot be read.
func (pm *Docker) Metrics(ctx context.Context, server *domain.Server) ([]domain.Metric, error) {
	now := time.Now()
	out := make([]domain.Metric, 0, 12)
	out = append(out, livenessMetric(server, now))

	if err := pm.ensureClient(ctx); err != nil {
		return out, nil
	}

	containerName := pm.resolveContainerName(ctx, server)

	resp, err := pm.client.ContainerStats(ctx, containerName, client.ContainerStatsOptions{
		IncludePreviousSample: true,
	})
	if err != nil {
		return out, nil
	}
	defer func() { _ = resp.Body.Close() }()

	var stats container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return out, errors.Wrap(err, "failed to decode container stats")
	}

	out = append(out, dockerStatsToMetrics(now, containerName, &stats)...)
	return out, nil
}

func dockerStatsToMetrics(ts time.Time, containerName string, stats *container.StatsResponse) []domain.Metric {
	labels := map[string]string{metricLabelContainer: containerName}
	out := make([]domain.Metric, 0, 8)

	if cpuPercent, ok := computeDockerCPUPercent(stats); ok {
		out = append(out, domain.Metric{
			Name:      metricServerCPUUsagePercent,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Float64Value(cpuPercent),
		})
	}

	memUsed := dockerEffectiveMemoryUsage(&stats.MemoryStats)
	out = append(out, domain.Metric{
		Name:      metricServerMemoryUsageBytes,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitBytes,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Uint64Value(memUsed),
	})
	if stats.MemoryStats.Limit > 0 {
		out = append(out,
			domain.Metric{
				Name:      metricServerMemoryLimitBytes,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitBytes,
				Labels:    cloneLabelMap(labels),
				Timestamp: ts,
				Value:     domain.Uint64Value(stats.MemoryStats.Limit),
			},
			domain.Metric{
				Name:      metricServerMemoryUsagePercent,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Labels:    cloneLabelMap(labels),
				Timestamp: ts,
				Value:     domain.Float64Value(float64(memUsed) / float64(stats.MemoryStats.Limit) * 100),
			},
		)
	}

	var rx, tx uint64
	for _, n := range stats.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	out = append(out,
		domain.Metric{
			Name:      metricServerNetworkReceiveBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(rx),
		},
		domain.Metric{
			Name:      metricServerNetworkTransmitBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(tx),
		},
	)

	read, write := dockerBlockIOTotals(&stats.BlkioStats)
	out = append(out,
		domain.Metric{
			Name:      metricServerBlockIOReadBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(read),
		},
		domain.Metric{
			Name:      metricServerBlockIOWriteBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(write),
		},
	)

	out = append(out, domain.Metric{
		Name:      metricServerProcessPIDs,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitCount,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Uint64Value(stats.PidsStats.Current),
	})

	return out
}

func computeDockerCPUPercent(stats *container.StatsResponse) (float64, bool) {
	// PreRead is unset when the daemon couldn't capture a prior sample
	// (e.g. container started < IncludePreviousSample interval ago).
	// Without a baseline the delta is meaningless, so suppress.
	if stats.PreRead.IsZero() {
		return 0, false
	}

	cpuTotal := stats.CPUStats.CPUUsage.TotalUsage
	preCPUTotal := stats.PreCPUStats.CPUUsage.TotalUsage
	// Counter went backwards (container restart between samples).
	if cpuTotal < preCPUTotal {
		return 0, false
	}
	// cpuDelta == 0 is legitimate: container was idle since prior sample.
	cpuDelta := float64(cpuTotal - preCPUTotal)

	system := stats.CPUStats.SystemUsage
	preSystem := stats.PreCPUStats.SystemUsage
	// Need a positive system-time delta to avoid division by zero. If
	// system time hasn't advanced, the two samples are from the same
	// instant or SystemUsage is unpopulated (Windows containers).
	if system <= preSystem {
		return 0, false
	}
	systemDelta := float64(system - preSystem)

	cores := float64(stats.CPUStats.OnlineCPUs)
	if cores == 0 {
		cores = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if cores == 0 {
		cores = 1
	}

	return (cpuDelta / systemDelta) * cores * 100, true
}

// dockerEffectiveMemoryUsage subtracts page cache from raw usage when the
// runtime exports it, matching `docker stats`. Falls back to the raw value
// when the stats map is missing the cache key (e.g. on Windows).
func dockerEffectiveMemoryUsage(m *container.MemoryStats) uint64 {
	if m.Usage == 0 {
		return 0
	}
	if cache, ok := m.Stats["cache"]; ok && cache <= m.Usage {
		return m.Usage - cache
	}
	return m.Usage
}

// dockerBlockIOTotals sums per-op block-IO bytes from cgroups v1 stats.
// Returns zeros when IoServiceBytesRecursive is empty (cgroups v2 or no IO
// since container start) — callers should still emit the metrics so the
// series stays continuous.
func dockerBlockIOTotals(b *container.BlkioStats) (read, write uint64) {
	for _, e := range b.IoServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			read += e.Value
		case "write":
			write += e.Value
		}
	}
	return read, write
}

func cloneLabelMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
