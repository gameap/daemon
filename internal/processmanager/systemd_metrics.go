//go:build linux

package processmanager

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

// systemdServiceStats is the parsed projection of `systemctl show` output.
// Pointer fields use nil to distinguish "property absent / [not set] /
// UINT64_MAX sentinel" from a real zero counter — only systemd's actual
// zero readings become *uint64(0).
type systemdServiceStats struct {
	MemoryCurrent  *uint64
	MemoryMax      *uint64
	CPUUsageNSec   *uint64
	IOReadBytes    *uint64
	IOWriteBytes   *uint64
	IPIngressBytes *uint64
	IPEgressBytes  *uint64
	TasksCurrent   *uint64
}

type systemdCPUSample struct {
	cpuNSec uint64
	at      time.Time
}

const systemdShowProperties = "MemoryCurrent,MemoryMax,CPUUsageNSec," +
	"IOReadBytes,IOWriteBytes,IPIngressBytes,IPEgressBytes,TasksCurrent"

// systemdUnsetSentinel is the value systemd emits for properties that have
// no current reading (e.g. MemoryCurrent when MemoryAccounting=off).
const systemdUnsetSentinel uint64 = 1<<64 - 1

// Metrics returns CPU, memory, network, block-IO and PID counters for the
// systemd unit backing this server. Falls back to the cached liveness gauge
// alone when the unit is unknown or `systemctl show` cannot be executed.
//
// Note: services created before the *Accounting=yes directives were added to
// `buildServiceConfig` will report zeros (and a suppressed CPU%) until the
// next start/restart regenerates the unit file.
func (pm *SystemD) Metrics(ctx context.Context, server *domain.Server) ([]domain.Metric, error) {
	now := time.Now()
	out := make([]domain.Metric, 0, 12)
	out = append(out, livenessMetric(server, now))

	serviceName := pm.resolveServiceName(server)

	stats, ok := pm.fetchSystemdStats(ctx, serviceName)
	if !ok {
		return out, nil
	}

	var cpuPercent float64
	var hasCPU bool
	if stats.CPUUsageNSec != nil {
		current := systemdCPUSample{cpuNSec: *stats.CPUUsageNSec, at: now}
		prior := pm.loadCPUSample(serviceName)
		cpuPercent, hasCPU = computeSystemdCPUPercent(prior, current)
		pm.recordCPUSample(serviceName, current)
	}

	out = append(out, systemdStatsToMetrics(now, serviceName, stats, cpuPercent, hasCPU)...)
	return out, nil
}

func (pm *SystemD) fetchSystemdStats(
	ctx context.Context, serviceName string,
) (systemdServiceStats, bool) {
	cmd := fmt.Sprintf("systemctl show %s --property=%s", serviceName, systemdShowProperties)
	output, code, err := pm.executor.Exec(ctx, cmd, contracts.ExecutorOptions{
		WorkDir: pm.cfg.WorkDir(),
	})
	if err != nil || code != 0 || len(output) == 0 {
		return systemdServiceStats{}, false
	}
	return parseSystemctlShow(output), true
}

func parseSystemctlShow(raw []byte) systemdServiceStats {
	stats := systemdServiceStats{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := line[:eq]
		val := line[eq+1:]
		switch key {
		case "MemoryCurrent":
			stats.MemoryCurrent = parseSystemdUintPtr(val)
		case "MemoryMax":
			if val == "infinity" {
				continue
			}
			stats.MemoryMax = parseSystemdUintPtr(val)
		case "CPUUsageNSec":
			stats.CPUUsageNSec = parseSystemdUintPtr(val)
		case "IOReadBytes":
			stats.IOReadBytes = parseSystemdUintPtr(val)
		case "IOWriteBytes":
			stats.IOWriteBytes = parseSystemdUintPtr(val)
		case "IPIngressBytes":
			stats.IPIngressBytes = parseSystemdUintPtr(val)
		case "IPEgressBytes":
			stats.IPEgressBytes = parseSystemdUintPtr(val)
		case "TasksCurrent":
			stats.TasksCurrent = parseSystemdUintPtr(val)
		}
	}
	return stats
}

// parseSystemdUintPtr returns nil for empty values, "[not set]", overflow,
// and the UINT64_MAX sentinel — all of which systemd uses to signal "no
// current reading".
func parseSystemdUintPtr(val string) *uint64 {
	if val == "" || val == "[not set]" {
		return nil
	}
	n, err := strconv.ParseUint(val, 10, 64)
	if err != nil {
		return nil
	}
	if n == systemdUnsetSentinel {
		return nil
	}
	return &n
}

// computeSystemdCPUPercent returns CPU as a fraction of one core (200% means
// two fully saturated cores), matching docker's convention. Suppresses
// emission when the prior sample is missing, the counter wrapped (service
// restarted between calls), or the wall delta is non-positive.
func computeSystemdCPUPercent(
	prior *systemdCPUSample, current systemdCPUSample,
) (float64, bool) {
	if prior == nil {
		return 0, false
	}
	if current.cpuNSec < prior.cpuNSec {
		return 0, false
	}
	wallDelta := current.at.Sub(prior.at).Nanoseconds()
	if wallDelta <= 0 {
		return 0, false
	}
	cpuDelta := current.cpuNSec - prior.cpuNSec
	return float64(cpuDelta) / float64(wallDelta) * 100, true
}

func (pm *SystemD) loadCPUSample(serviceName string) *systemdCPUSample {
	pm.cpuSamplesMu.Lock()
	defer pm.cpuSamplesMu.Unlock()
	s, ok := pm.cpuSamples[serviceName]
	if !ok {
		return nil
	}
	return &s
}

func (pm *SystemD) recordCPUSample(serviceName string, sample systemdCPUSample) {
	pm.cpuSamplesMu.Lock()
	defer pm.cpuSamplesMu.Unlock()
	pm.cpuSamples[serviceName] = sample
}

func systemdStatsToMetrics(
	ts time.Time,
	serviceName string,
	stats systemdServiceStats,
	cpuPercent float64, hasCPU bool,
) []domain.Metric {
	labels := map[string]string{metricLabelService: serviceName}
	out := make([]domain.Metric, 0, 8)

	if hasCPU {
		out = append(out, domain.Metric{
			Name:      metricServerCPUUsagePercent,
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Float64Value(cpuPercent),
		})
	}

	memUsed := uint64Or0(stats.MemoryCurrent)
	out = append(out, domain.Metric{
		Name:      metricServerMemoryUsageBytes,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitBytes,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Uint64Value(memUsed),
	})
	if stats.MemoryMax != nil && *stats.MemoryMax > 0 {
		out = append(out,
			domain.Metric{
				Name:      metricServerMemoryLimitBytes,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitBytes,
				Labels:    cloneLabelMap(labels),
				Timestamp: ts,
				Value:     domain.Uint64Value(*stats.MemoryMax),
			},
			domain.Metric{
				Name:      metricServerMemoryUsagePercent,
				Type:      domain.MetricTypeGauge,
				Unit:      domain.MetricUnitPercent,
				Labels:    cloneLabelMap(labels),
				Timestamp: ts,
				Value:     domain.Float64Value(float64(memUsed) / float64(*stats.MemoryMax) * 100),
			},
		)
	}

	out = append(out,
		domain.Metric{
			Name:      metricServerNetworkReceiveBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(uint64Or0(stats.IPIngressBytes)),
		},
		domain.Metric{
			Name:      metricServerNetworkTransmitBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(uint64Or0(stats.IPEgressBytes)),
		},
		domain.Metric{
			Name:      metricServerBlockIOReadBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(uint64Or0(stats.IOReadBytes)),
		},
		domain.Metric{
			Name:      metricServerBlockIOWriteBytesTotal,
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Labels:    cloneLabelMap(labels),
			Timestamp: ts,
			Value:     domain.Uint64Value(uint64Or0(stats.IOWriteBytes)),
		},
	)

	out = append(out, domain.Metric{
		Name:      metricServerProcessPIDs,
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitCount,
		Labels:    cloneLabelMap(labels),
		Timestamp: ts,
		Value:     domain.Uint64Value(uint64Or0(stats.TasksCurrent)),
	})

	return out
}

func uint64Or0(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
