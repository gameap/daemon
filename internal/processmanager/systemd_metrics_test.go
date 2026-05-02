//go:build linux

package processmanager

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor is a minimal contracts.Executor double for the metrics tests.
// Only Exec is exercised — ExecWithWriter exists to satisfy the interface.
type fakeExecutor struct {
	output []byte
	code   int
	err    error

	gotCommand string
	calls      int
}

func (f *fakeExecutor) Exec(_ context.Context, command string, _ contracts.ExecutorOptions) ([]byte, int, error) {
	f.gotCommand = command
	f.calls++
	return f.output, f.code, f.err
}

func (f *fakeExecutor) ExecWithWriter(_ context.Context, _ string, _ io.Writer, _ contracts.ExecutorOptions) (int, error) {
	return 0, nil
}

func ptrUint64(v uint64) *uint64 { return &v }

func TestParseSystemctlShow_HappyPath(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"MemoryCurrent=104857600",
		"MemoryMax=2147483648",
		"CPUUsageNSec=987654321",
		"IOReadBytes=1024",
		"IOWriteBytes=2048",
		"IPIngressBytes=512",
		"IPEgressBytes=256",
		"TasksCurrent=42",
	}, "\n"))

	got := parseSystemctlShow(raw)

	assert.Equal(t, ptrUint64(104857600), got.MemoryCurrent)
	assert.Equal(t, ptrUint64(2147483648), got.MemoryMax)
	assert.Equal(t, ptrUint64(987654321), got.CPUUsageNSec)
	assert.Equal(t, ptrUint64(1024), got.IOReadBytes)
	assert.Equal(t, ptrUint64(2048), got.IOWriteBytes)
	assert.Equal(t, ptrUint64(512), got.IPIngressBytes)
	assert.Equal(t, ptrUint64(256), got.IPEgressBytes)
	assert.Equal(t, ptrUint64(42), got.TasksCurrent)
}

func TestParseSystemctlShow_MemoryMaxInfinity(t *testing.T) {
	got := parseSystemctlShow([]byte("MemoryMax=infinity\n"))
	assert.Nil(t, got.MemoryMax, "infinity must be treated as no limit")
}

func TestParseSystemctlShow_NotSet(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"MemoryCurrent=[not set]",
		"CPUUsageNSec=[not set]",
		"IOReadBytes=[not set]",
		"TasksCurrent=[not set]",
	}, "\n"))

	got := parseSystemctlShow(raw)

	assert.Nil(t, got.MemoryCurrent)
	assert.Nil(t, got.CPUUsageNSec)
	assert.Nil(t, got.IOReadBytes)
	assert.Nil(t, got.TasksCurrent)
}

func TestParseSystemctlShow_UnsetSentinel(t *testing.T) {
	raw := []byte("MemoryCurrent=18446744073709551615\nCPUUsageNSec=18446744073709551615\n")

	got := parseSystemctlShow(raw)

	assert.Nil(t, got.MemoryCurrent, "UINT64_MAX is systemd's unset sentinel")
	assert.Nil(t, got.CPUUsageNSec)
}

func TestParseSystemctlShow_PropertyAbsent(t *testing.T) {
	got := parseSystemctlShow([]byte("MemoryCurrent=1024\n"))

	assert.Equal(t, ptrUint64(1024), got.MemoryCurrent)
	assert.Nil(t, got.MemoryMax)
	assert.Nil(t, got.CPUUsageNSec)
	assert.Nil(t, got.TasksCurrent)
}

func TestParseSystemctlShow_CRLFTolerance(t *testing.T) {
	raw := []byte("MemoryCurrent=1024\r\nCPUUsageNSec=2048\r\n")

	got := parseSystemctlShow(raw)

	assert.Equal(t, ptrUint64(1024), got.MemoryCurrent)
	assert.Equal(t, ptrUint64(2048), got.CPUUsageNSec)
}

func TestParseSystemctlShow_GarbageLineIgnored(t *testing.T) {
	raw := []byte("garbage_without_equals\nMemoryCurrent=1024\nUnknownKey=999\n")

	got := parseSystemctlShow(raw)

	assert.Equal(t, ptrUint64(1024), got.MemoryCurrent)
}

func TestParseSystemctlShow_OverflowReturnsNil(t *testing.T) {
	raw := []byte("MemoryCurrent=99999999999999999999\n")

	got := parseSystemctlShow(raw)

	assert.Nil(t, got.MemoryCurrent, "overflow must produce nil, not panic")
}

func TestParseSystemctlShow_ValueWithEqualsSign(t *testing.T) {
	// Synthetic: a value containing '=' must be split on the first occurrence.
	// The KEY won't actually be one of ours, but the parser must not crash.
	raw := []byte("CustomEnv=KEY=VALUE\nMemoryCurrent=1024\n")

	got := parseSystemctlShow(raw)

	assert.Equal(t, ptrUint64(1024), got.MemoryCurrent)
}

func TestComputeSystemdCPUPercent_NoPriorSampleSuppresses(t *testing.T) {
	current := systemdCPUSample{cpuNSec: 1_000_000_000, at: time.Now()}

	_, ok := computeSystemdCPUPercent(nil, current)
	assert.False(t, ok, "first sample → suppress")
}

func TestComputeSystemdCPUPercent_CounterBackwardsSuppresses(t *testing.T) {
	now := time.Now()
	prior := &systemdCPUSample{cpuNSec: 5_000_000_000, at: now.Add(-time.Second)}
	current := systemdCPUSample{cpuNSec: 1_000_000_000, at: now}

	_, ok := computeSystemdCPUPercent(prior, current)
	assert.False(t, ok, "service restart → counter went backwards → suppress")
}

func TestComputeSystemdCPUPercent_ZeroWallDeltaSuppresses(t *testing.T) {
	now := time.Now()
	prior := &systemdCPUSample{cpuNSec: 1_000_000_000, at: now}
	current := systemdCPUSample{cpuNSec: 2_000_000_000, at: now}

	_, ok := computeSystemdCPUPercent(prior, current)
	assert.False(t, ok, "duplicate-instant samples → suppress")
}

func TestComputeSystemdCPUPercent_IdleEmitsZero(t *testing.T) {
	now := time.Now()
	prior := &systemdCPUSample{cpuNSec: 1_000_000_000, at: now.Add(-time.Second)}
	current := systemdCPUSample{cpuNSec: 1_000_000_000, at: now}

	got, ok := computeSystemdCPUPercent(prior, current)
	assert.True(t, ok, "idle service with prior sample must emit a percent")
	assert.Equal(t, 0.0, got, "idle CPU must be 0%, not suppressed")
}

func TestComputeSystemdCPUPercent_OneCoreSaturated(t *testing.T) {
	now := time.Now()
	prior := &systemdCPUSample{cpuNSec: 1_000_000_000, at: now.Add(-time.Second)}
	current := systemdCPUSample{cpuNSec: 2_000_000_000, at: now}

	got, ok := computeSystemdCPUPercent(prior, current)
	require.True(t, ok)
	assert.InDelta(t, 100.0, got, 0.001, "1s of CPU time over 1s wall clock = 100%")
}

func TestComputeSystemdCPUPercent_TwoCoresSaturated(t *testing.T) {
	now := time.Now()
	prior := &systemdCPUSample{cpuNSec: 1_000_000_000, at: now.Add(-time.Second)}
	current := systemdCPUSample{cpuNSec: 3_000_000_000, at: now}

	got, ok := computeSystemdCPUPercent(prior, current)
	require.True(t, ok)
	assert.InDelta(t, 200.0, got, 0.001, "2s of CPU time over 1s wall clock = 200%")
}

func TestSystemdStatsToMetrics_AlwaysEmitsPIDs(t *testing.T) {
	got := systemdStatsToMetrics(time.Now(), "svc.service", systemdServiceStats{}, 0, false)

	pids := collectByName(got, metricServerProcessPIDs)
	if assert.Len(t, pids, 1, "PIDs gauge must be emitted even when TasksCurrent is nil") {
		assert.Equal(t, uint64(0), pids[0].Value.Uint64())
	}
}

func TestSystemdStatsToMetrics_AlwaysEmitsCounters(t *testing.T) {
	got := systemdStatsToMetrics(time.Now(), "svc.service", systemdServiceStats{}, 0, false)

	for _, name := range []string{
		metricServerNetworkReceiveBytesTotal,
		metricServerNetworkTransmitBytesTotal,
		metricServerBlockIOReadBytesTotal,
		metricServerBlockIOWriteBytesTotal,
	} {
		entries := collectByName(got, name)
		if assert.Len(t, entries, 1, "metric %q must be emitted with zero value", name) {
			assert.Equal(t, uint64(0), entries[0].Value.Uint64())
		}
	}
}

func TestSystemdStatsToMetrics_MemoryUsageWithoutLimit(t *testing.T) {
	stats := systemdServiceStats{MemoryCurrent: ptrUint64(1024)}

	got := systemdStatsToMetrics(time.Now(), "svc.service", stats, 0, false)

	used := collectByName(got, metricServerMemoryUsageBytes)
	if assert.Len(t, used, 1, "memory_usage_bytes must be emitted even without a limit") {
		assert.Equal(t, uint64(1024), used[0].Value.Uint64())
	}
	assert.Empty(t, collectByName(got, metricServerMemoryLimitBytes),
		"memory_limit_bytes must be omitted when no limit is set")
	assert.Empty(t, collectByName(got, metricServerMemoryUsagePercent),
		"memory_usage_percent must be omitted when no limit is set")
}

func TestSystemdStatsToMetrics_MemoryWithLimitEmitsAllThree(t *testing.T) {
	stats := systemdServiceStats{
		MemoryCurrent: ptrUint64(0),
		MemoryMax:     ptrUint64(1 << 30),
	}

	got := systemdStatsToMetrics(time.Now(), "svc.service", stats, 0, false)

	used := collectByName(got, metricServerMemoryUsageBytes)
	if assert.Len(t, used, 1) {
		assert.Equal(t, uint64(0), used[0].Value.Uint64())
	}
	assert.Len(t, collectByName(got, metricServerMemoryLimitBytes), 1)
	pct := collectByName(got, metricServerMemoryUsagePercent)
	if assert.Len(t, pct, 1) {
		assert.Equal(t, 0.0, pct[0].Value.Float64())
	}
}

func TestSystemdStatsToMetrics_MemoryPercentComputedAgainstLimit(t *testing.T) {
	stats := systemdServiceStats{
		MemoryCurrent: ptrUint64(512),
		MemoryMax:     ptrUint64(1024),
	}

	got := systemdStatsToMetrics(time.Now(), "svc.service", stats, 0, false)

	pct := collectByName(got, metricServerMemoryUsagePercent)
	require.Len(t, pct, 1)
	assert.InDelta(t, 50.0, pct[0].Value.Float64(), 0.001)
}

func TestSystemdStatsToMetrics_CPUSuppressedWhenHasCPUFalse(t *testing.T) {
	got := systemdStatsToMetrics(time.Now(), "svc.service", systemdServiceStats{}, 99.0, false)

	assert.Empty(t, collectByName(got, metricServerCPUUsagePercent),
		"cpu_usage_percent must be omitted when hasCPU=false")
}

func TestSystemdStatsToMetrics_CPUEmittedAsZeroWhenHasCPUTrue(t *testing.T) {
	got := systemdStatsToMetrics(time.Now(), "svc.service", systemdServiceStats{}, 0, true)

	cpu := collectByName(got, metricServerCPUUsagePercent)
	if assert.Len(t, cpu, 1, "cpu_usage_percent must be emitted with 0 when idle") {
		assert.Equal(t, 0.0, cpu[0].Value.Float64())
	}
}

func TestSystemdStatsToMetrics_ServiceLabel(t *testing.T) {
	got := systemdStatsToMetrics(time.Now(), "gameap-server-abc.service", systemdServiceStats{}, 0, false)

	require.NotEmpty(t, got)
	for _, m := range got {
		if m.Name == metricServerUp {
			continue // liveness has no service label
		}
		assert.Equal(t, "gameap-server-abc.service", m.Labels[metricLabelService],
			"every per-stats metric must carry the service label")
	}
}

func TestSystemdStatsToMetrics_PopulatedCounters(t *testing.T) {
	stats := systemdServiceStats{
		IOReadBytes:    ptrUint64(111),
		IOWriteBytes:   ptrUint64(222),
		IPIngressBytes: ptrUint64(333),
		IPEgressBytes:  ptrUint64(444),
		TasksCurrent:   ptrUint64(5),
	}

	got := systemdStatsToMetrics(time.Now(), "svc.service", stats, 0, false)

	cases := map[string]uint64{
		metricServerBlockIOReadBytesTotal:     111,
		metricServerBlockIOWriteBytesTotal:    222,
		metricServerNetworkReceiveBytesTotal:  333,
		metricServerNetworkTransmitBytesTotal: 444,
		metricServerProcessPIDs:               5,
	}
	for name, want := range cases {
		entries := collectByName(got, name)
		if assert.Len(t, entries, 1, "metric %q", name) {
			assert.Equal(t, want, entries[0].Value.Uint64(), "metric %q value", name)
		}
	}
}

func TestSystemDMetrics_ExecutorErrorYieldsLivenessOnly(t *testing.T) {
	exec := &fakeExecutor{err: assert.AnError}
	pm := NewSystemD(&config.Config{}, nil, exec)

	got, err := pm.Metrics(context.Background(), makeTestServer())

	require.NoError(t, err, "stats unavailable must not propagate as an error")
	assert.Len(t, got, 1, "only the liveness metric must be emitted")
	assert.Equal(t, metricServerUp, got[0].Name)
}

func TestSystemDMetrics_NonZeroExitCodeYieldsLivenessOnly(t *testing.T) {
	exec := &fakeExecutor{code: 4} // statusServiceUnknown
	pm := NewSystemD(&config.Config{}, nil, exec)

	got, err := pm.Metrics(context.Background(), makeTestServer())

	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, metricServerUp, got[0].Name)
}

func TestSystemDMetrics_FirstCallSuppressesCPUAndStoresSample(t *testing.T) {
	exec := &fakeExecutor{
		code: 0,
		output: []byte(strings.Join([]string{
			"MemoryCurrent=2048",
			"MemoryMax=4096",
			"CPUUsageNSec=1000000000",
			"IOReadBytes=10",
			"IOWriteBytes=20",
			"IPIngressBytes=30",
			"IPEgressBytes=40",
			"TasksCurrent=3",
		}, "\n")),
	}
	pm := NewSystemD(&config.Config{}, nil, exec)
	server := makeTestServer()

	got, err := pm.Metrics(context.Background(), server)

	require.NoError(t, err)
	assert.Empty(t, collectByName(got, metricServerCPUUsagePercent),
		"first call has no prior sample → CPU% suppressed")
	assert.Len(t, collectByName(got, metricServerMemoryUsageBytes), 1)
	assert.Len(t, collectByName(got, metricServerMemoryLimitBytes), 1)
	assert.Len(t, collectByName(got, metricServerProcessPIDs), 1)

	pm.cpuSamplesMu.Lock()
	defer pm.cpuSamplesMu.Unlock()
	assert.Len(t, pm.cpuSamples, 1, "first call must store the CPU sample for next call")
}

func TestSystemDMetrics_SecondCallEmitsCPU(t *testing.T) {
	exec := &fakeExecutor{
		code:   0,
		output: []byte("CPUUsageNSec=1000000000\nMemoryCurrent=0\n"),
	}
	pm := NewSystemD(&config.Config{}, nil, exec)
	server := makeTestServer()
	serviceName := pm.resolveServiceName(server)

	// Seed prior sample as if from a previous call one second ago.
	pm.recordCPUSample(serviceName, systemdCPUSample{
		cpuNSec: 0,
		at:      time.Now().Add(-time.Second),
	})

	got, err := pm.Metrics(context.Background(), server)

	require.NoError(t, err)
	cpu := collectByName(got, metricServerCPUUsagePercent)
	if assert.Len(t, cpu, 1, "CPU% must be emitted once a prior sample exists") {
		// 1e9 ns over ~1s wall clock ≈ 100%, but exact timing is fuzzy in tests.
		assert.Greater(t, cpu[0].Value.Float64(), 0.0)
	}
}

func TestSystemDMetrics_CommandIncludesServiceAndProperties(t *testing.T) {
	exec := &fakeExecutor{code: 0, output: []byte("MemoryCurrent=0\n")}
	pm := NewSystemD(&config.Config{}, nil, exec)

	_, err := pm.Metrics(context.Background(), makeTestServer())
	require.NoError(t, err)

	assert.Contains(t, exec.gotCommand, "systemctl show")
	assert.Contains(t, exec.gotCommand, "--property=")
	assert.Contains(t, exec.gotCommand, "CPUUsageNSec")
	assert.Contains(t, exec.gotCommand, "MemoryCurrent")
}

func makeTestServer() *domain.Server {
	return domain.NewServer(
		1337,
		true,
		domain.ServerInstalled,
		false,
		"name",
		"759b875e-d910-11eb-aff7-d796d7fcf7ef",
		"759b875e",
		domain.Game{StartCode: "cstrike"},
		domain.GameMod{Name: "public"},
		"1.3.3.7",
		1337,
		1338,
		1339,
		"paS$w0rD",
		"",
		"gameap-user",
		"./run.sh",
		"",
		"",
		"",
		true,
		time.Now(),
		map[string]string{},
		map[string]string{},
		time.Now(),
		0,
		0,
	)
}
