//go:build linux || darwin || windows

package processmanager

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
)

func collectByName(metrics []domain.Metric, name string) []domain.Metric {
	out := make([]domain.Metric, 0, 2)
	for i := range metrics {
		if metrics[i].Name == name {
			out = append(out, metrics[i])
		}
	}
	return out
}

func TestComputeDockerCPUPercent_IdleEmitsZero(t *testing.T) {
	stats := &container.StatsResponse{
		PreRead: time.Now().Add(-time.Second),
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 10_000_000,
			OnlineCPUs:  4,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 9_000_000,
		},
	}

	got, ok := computeDockerCPUPercent(stats)
	assert.True(t, ok, "idle container with prior sample must emit a percent")
	assert.Equal(t, 0.0, got, "idle CPU must be 0%, not suppressed")
}

func TestComputeDockerCPUPercent_NoPriorSampleSuppresses(t *testing.T) {
	stats := &container.StatsResponse{
		// PreRead intentionally zero.
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 10_000_000,
		},
	}

	_, ok := computeDockerCPUPercent(stats)
	assert.False(t, ok, "no prior sample → suppress (delta is undefined)")
}

func TestComputeDockerCPUPercent_CounterResetSuppresses(t *testing.T) {
	stats := &container.StatsResponse{
		PreRead: time.Now().Add(-time.Second),
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 100},
			SystemUsage: 10_000_000,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 9_000_000,
		},
	}

	_, ok := computeDockerCPUPercent(stats)
	assert.False(t, ok, "counter went backwards → suppress")
}

func TestComputeDockerCPUPercent_NormalCase(t *testing.T) {
	stats := &container.StatsResponse{
		PreRead: time.Now().Add(-time.Second),
		CPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 2_000_000},
			SystemUsage: 12_000_000,
			OnlineCPUs:  2,
		},
		PreCPUStats: container.CPUStats{
			CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000},
			SystemUsage: 11_000_000,
		},
	}

	got, ok := computeDockerCPUPercent(stats)
	assert.True(t, ok)
	// (1M / 1M) * 2 * 100 = 200%
	assert.InDelta(t, 200.0, got, 0.0001)
}

func TestDockerStatsToMetrics_AlwaysEmitsPIDs(t *testing.T) {
	stats := &container.StatsResponse{
		PidsStats: container.PidsStats{Current: 0},
	}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	pids := collectByName(got, metricServerProcessPIDs)
	if assert.Len(t, pids, 1, "PIDs gauge must be emitted even when 0") {
		assert.Equal(t, uint64(0), pids[0].Value.Uint64())
	}
}

func TestDockerStatsToMetrics_EmitsZeroBytesNetwork(t *testing.T) {
	stats := &container.StatsResponse{
		Networks: map[string]container.NetworkStats{
			"eth0": {RxBytes: 0, TxBytes: 0},
		},
	}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	rx := collectByName(got, metricServerNetworkReceiveBytesTotal)
	tx := collectByName(got, metricServerNetworkTransmitBytesTotal)
	if assert.Len(t, rx, 1) && assert.Len(t, tx, 1) {
		assert.Equal(t, uint64(0), rx[0].Value.Uint64())
		assert.Equal(t, uint64(0), tx[0].Value.Uint64())
	}
}

func TestDockerStatsToMetrics_EmitsMemoryWithLimit(t *testing.T) {
	stats := &container.StatsResponse{
		MemoryStats: container.MemoryStats{
			Usage: 0,
			Limit: 1 << 30,
		},
	}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	used := collectByName(got, metricServerMemoryUsageBytes)
	if assert.Len(t, used, 1, "memory_usage_bytes must be emitted alongside limit") {
		assert.Equal(t, uint64(0), used[0].Value.Uint64())
	}
	assert.Len(t, collectByName(got, metricServerMemoryLimitBytes), 1)
	assert.Len(t, collectByName(got, metricServerMemoryUsagePercent), 1)
}

func TestPodmanStatsToMetrics_AlwaysEmitsPIDs(t *testing.T) {
	got := podmanStatsToMetrics(time.Now(), "c1", &podmanStatsEntry{PIDs: 0})

	pids := collectByName(got, metricServerProcessPIDs)
	if assert.Len(t, pids, 1, "PIDs gauge must be emitted even when 0") {
		assert.Equal(t, uint64(0), pids[0].Value.Uint64())
	}
}

func TestPodmanStatsToMetrics_EmitsZeroValueCountersAndCPU(t *testing.T) {
	got := podmanStatsToMetrics(time.Now(), "c1", &podmanStatsEntry{
		CPU: 0, NetInput: 0, NetOutput: 0, BlockInput: 0, BlockOutput: 0,
	})

	for _, name := range []string{
		metricServerCPUUsagePercent,
		metricServerNetworkReceiveBytesTotal,
		metricServerNetworkTransmitBytesTotal,
		metricServerBlockIOReadBytesTotal,
		metricServerBlockIOWriteBytesTotal,
	} {
		assert.Len(t, collectByName(got, name), 1, "metric %q must be emitted with zero value", name)
	}
}

func TestDockerStatsToMetrics_AlwaysEmitsBlockIO(t *testing.T) {
	// Empty BlkioStats happens with cgroups v2 or before any IO has occurred.
	stats := &container.StatsResponse{}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	read := collectByName(got, metricServerBlockIOReadBytesTotal)
	write := collectByName(got, metricServerBlockIOWriteBytesTotal)
	if assert.Len(t, read, 1, "block_io_read must be emitted even when stats are empty") {
		assert.Equal(t, uint64(0), read[0].Value.Uint64())
	}
	if assert.Len(t, write, 1, "block_io_write must be emitted even when stats are empty") {
		assert.Equal(t, uint64(0), write[0].Value.Uint64())
	}
}

func TestDockerStatsToMetrics_AlwaysEmitsNetwork(t *testing.T) {
	// Container with --network none has no Networks entries.
	stats := &container.StatsResponse{}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	rx := collectByName(got, metricServerNetworkReceiveBytesTotal)
	tx := collectByName(got, metricServerNetworkTransmitBytesTotal)
	if assert.Len(t, rx, 1, "network_receive must be emitted with no interfaces") {
		assert.Equal(t, uint64(0), rx[0].Value.Uint64())
	}
	if assert.Len(t, tx, 1, "network_transmit must be emitted with no interfaces") {
		assert.Equal(t, uint64(0), tx[0].Value.Uint64())
	}
}

func TestDockerStatsToMetrics_EmitsMemoryUsageWhenUsageAndLimitZero(t *testing.T) {
	stats := &container.StatsResponse{
		MemoryStats: container.MemoryStats{Usage: 0, Limit: 0},
	}
	got := dockerStatsToMetrics(time.Now(), "c1", stats)

	used := collectByName(got, metricServerMemoryUsageBytes)
	if assert.Len(t, used, 1, "memory_usage_bytes must be emitted even without a limit") {
		assert.Equal(t, uint64(0), used[0].Value.Uint64())
	}
	assert.Empty(t, collectByName(got, metricServerMemoryLimitBytes),
		"memory_limit_bytes must be omitted when no limit is set")
	assert.Empty(t, collectByName(got, metricServerMemoryUsagePercent),
		"memory_usage_percent must be omitted when no limit is set")
}

func TestPodmanStatsToMetrics_EmitsMemoryUsageWhenUsageAndLimitZero(t *testing.T) {
	got := podmanStatsToMetrics(time.Now(), "c1", &podmanStatsEntry{MemUsage: 0, MemLimit: 0})

	used := collectByName(got, metricServerMemoryUsageBytes)
	if assert.Len(t, used, 1, "memory_usage_bytes must be emitted even without a limit") {
		assert.Equal(t, uint64(0), used[0].Value.Uint64())
	}
	assert.Empty(t, collectByName(got, metricServerMemoryLimitBytes),
		"memory_limit_bytes must be omitted when no limit is set")
	assert.Empty(t, collectByName(got, metricServerMemoryUsagePercent),
		"memory_usage_percent must be omitted when no limit is set")
}
