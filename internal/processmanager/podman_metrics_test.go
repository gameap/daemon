//go:build linux || darwin

package processmanager

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
