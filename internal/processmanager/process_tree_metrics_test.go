package processmanager

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var usageBase = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func cpuTicks(d time.Duration) int64 {
	return int64(d / filetimeTick)
}

// startedAt is the creation time of a process started d after usageBase.
func startedAt(d time.Duration) int64 {
	return filetimeTicks(usageBase.Add(d))
}

func TestFiletimeTicks(t *testing.T) {
	assert.Equal(t, int64(116444736000000000), filetimeTicks(time.Unix(0, 0)))
	assert.Equal(t, int64(116444736000000000+10_000_000), filetimeTicks(time.Unix(1, 0)))
}

func TestComputeProcessTreeUsage_FirstObservation(t *testing.T) {
	tree := []processInfo{
		{
			PID: 3100, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(5 * time.Second), Threads: 1,
			PrivateWorkingSet: 2 << 20, ReadBytes: 100, WriteBytes: 10,
		},
		{
			PID: 3180, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(90 * time.Second), Threads: 24,
			PrivateWorkingSet: 700 << 20, ReadBytes: 5000, WriteBytes: 800,
		},
	}

	usage, sample := computeProcessTreeUsage(nil, tree, usageBase)

	assert.Equal(t, processTreeUsage{
		PrivateWorkingSet: 702 << 20,
		ReadBytes:         5100,
		WriteBytes:        810,
		Threads:           25,
	}, usage)
	assert.Equal(t, usageBase, sample.at)
	assert.Equal(t, uint64(5100), sample.readBytes)
	assert.Equal(t, uint64(810), sample.writeBytes)
	assert.Equal(t, map[processKey]processCounters{
		{pid: 3100, createTime: startedAt(-time.Hour)}: {cpuTime: cpuTicks(5 * time.Second), readBytes: 100, writeBytes: 10},
		{pid: 3180, createTime: startedAt(-time.Hour)}: {cpuTime: cpuTicks(90 * time.Second), readBytes: 5000, writeBytes: 800},
	}, sample.counters)
}

func TestComputeProcessTreeUsage_AgainstPriorObservation(t *testing.T) {
	game := processInfo{
		PID: 3180, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(90 * time.Second),
		Threads: 24, PrivateWorkingSet: 700 << 20, ReadBytes: 5000, WriteBytes: 800,
	}
	helper := processInfo{
		PID: 3300, CreateTime: startedAt(-time.Minute), CPUTime: cpuTicks(2 * time.Second),
		Threads: 2, PrivateWorkingSet: 10 << 20, ReadBytes: 300, WriteBytes: 30,
	}

	grown := func(p processInfo, cpu time.Duration, read, write uint64) processInfo {
		p.CPUTime += cpuTicks(cpu)
		p.ReadBytes += read
		p.WriteBytes += write

		return p
	}

	tests := []struct {
		name      string
		prior     []processInfo
		current   []processInfo
		wall      time.Duration
		wantCPU   float64
		wantRead  uint64
		wantWrite uint64
	}{
		{
			name:      "idle",
			prior:     []processInfo{game},
			current:   []processInfo{game},
			wall:      time.Second,
			wantCPU:   0,
			wantRead:  5000,
			wantWrite: 800,
		},
		{
			name:      "one_core",
			prior:     []processInfo{game},
			current:   []processInfo{grown(game, 5*time.Second, 64, 16)},
			wall:      5 * time.Second,
			wantCPU:   100,
			wantRead:  5064,
			wantWrite: 816,
		},
		{
			name:      "several_cores",
			prior:     []processInfo{game, helper},
			current:   []processInfo{grown(game, 2*time.Second, 0, 0), grown(helper, time.Second, 0, 0)},
			wall:      time.Second,
			wantCPU:   300,
			wantRead:  5300,
			wantWrite: 830,
		},
		{
			name:  "process_started_after_the_prior_observation_counts_in_full",
			prior: []processInfo{game},
			current: []processInfo{game, {
				PID: 3400, CreateTime: startedAt(500 * time.Millisecond), CPUTime: cpuTicks(500 * time.Millisecond),
				ReadBytes: 64, WriteBytes: 8,
			}},
			wall:      time.Second,
			wantCPU:   50,
			wantRead:  5064,
			wantWrite: 808,
		},
		{
			name:      "process_older_than_the_prior_observation_has_no_baseline",
			prior:     []processInfo{game},
			current:   []processInfo{game, helper},
			wall:      time.Second,
			wantCPU:   0,
			wantRead:  5000,
			wantWrite: 800,
		},
		{
			name:  "reused_process_id_counts_as_a_new_process",
			prior: []processInfo{game},
			current: []processInfo{{
				PID: game.PID, CreateTime: startedAt(750 * time.Millisecond), CPUTime: cpuTicks(250 * time.Millisecond),
				ReadBytes: 10, WriteBytes: 1,
			}},
			wall:      time.Second,
			wantCPU:   25,
			wantRead:  5010,
			wantWrite: 801,
		},
		{
			name:  "counters_going_back_are_clamped",
			prior: []processInfo{game},
			current: []processInfo{{
				PID: game.PID, CreateTime: game.CreateTime, CPUTime: game.CPUTime - cpuTicks(time.Second),
				ReadBytes: game.ReadBytes - 100, WriteBytes: game.WriteBytes - 10,
			}},
			wall:      time.Second,
			wantCPU:   0,
			wantRead:  5000,
			wantWrite: 800,
		},
		{
			name:      "exited_process_keeps_its_share_of_the_io_totals",
			prior:     []processInfo{game, helper},
			current:   []processInfo{grown(game, time.Second, 100, 10)},
			wall:      2 * time.Second,
			wantCPU:   50,
			wantRead:  5400,
			wantWrite: 840,
		},
		{
			name:  "service_restarted_between_observations",
			prior: []processInfo{game, helper},
			current: []processInfo{{
				PID: 4100, CreateTime: startedAt(2 * time.Second), CPUTime: cpuTicks(time.Second),
				ReadBytes: 50, WriteBytes: 5,
			}},
			wall:      4 * time.Second,
			wantCPU:   25,
			wantRead:  5350,
			wantWrite: 835,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, prior := computeProcessTreeUsage(nil, tt.prior, usageBase)

			usage, next := computeProcessTreeUsage(&prior, tt.current, usageBase.Add(tt.wall))

			require.True(t, usage.HasCPU)
			assert.InDelta(t, tt.wantCPU, usage.CPUPercent, 0.001)
			assert.Equal(t, tt.wantRead, usage.ReadBytes)
			assert.Equal(t, tt.wantWrite, usage.WriteBytes)
			assert.Equal(t, tt.wantRead, next.readBytes)
			assert.Equal(t, tt.wantWrite, next.writeBytes)
			assert.Len(t, next.counters, len(tt.current))
		})
	}
}

func TestComputeProcessTreeUsage_NoIntervalReportsNoCPU(t *testing.T) {
	tree := []processInfo{{PID: 3180, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(time.Minute)}}

	_, prior := computeProcessTreeUsage(nil, tree, usageBase)

	sameInstant, _ := computeProcessTreeUsage(&prior, tree, usageBase)
	assert.False(t, sameInstant.HasCPU)

	clockSetBack, _ := computeProcessTreeUsage(&prior, tree, usageBase.Add(-time.Second))
	assert.False(t, clockSetBack.HasCPU)
}

func TestProcessTreeSampler_MeasuresEachServiceAgainstItsOwnPast(t *testing.T) {
	sampler := newProcessTreeSampler()

	game := processInfo{PID: 3180, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(time.Minute)}
	other := processInfo{PID: 5120, CreateTime: startedAt(-time.Hour), CPUTime: cpuTicks(time.Hour)}

	assert.False(t, sampler.observe("gameapServer1", []processInfo{game}, usageBase).HasCPU)
	assert.False(t, sampler.observe("gameapServer2", []processInfo{other}, usageBase).HasCPU)

	game.CPUTime += cpuTicks(time.Second)
	usage := sampler.observe("gameapServer1", []processInfo{game}, usageBase.Add(2*time.Second))

	require.True(t, usage.HasCPU)
	assert.InDelta(t, 50.0, usage.CPUPercent, 0.001)
}

func TestProcessTreeSampler_ForgetStartsOver(t *testing.T) {
	sampler := newProcessTreeSampler()

	game := processInfo{PID: 3180, CreateTime: startedAt(-time.Hour), ReadBytes: 100, WriteBytes: 10}
	sampler.observe("gameapServer3", []processInfo{game}, usageBase)

	sampler.forget("gameapServer3")

	restarted := processInfo{PID: 4100, CreateTime: startedAt(time.Second), ReadBytes: 40, WriteBytes: 4}
	usage := sampler.observe("gameapServer3", []processInfo{restarted}, usageBase.Add(2*time.Second))

	assert.False(t, usage.HasCPU)
	assert.Equal(t, uint64(40), usage.ReadBytes)
	assert.Equal(t, uint64(4), usage.WriteBytes)
}

func TestProcessTreeSampler_ConcurrentObservations(t *testing.T) {
	sampler := newProcessTreeSampler()

	var wg sync.WaitGroup

	for worker := range 8 {
		wg.Go(func() {
			service := "gameapServer" + strconv.Itoa(worker%3)

			for step := range 50 {
				tree := []processInfo{{
					PID:        uint32(3000 + worker),
					CreateTime: startedAt(-time.Hour),
					CPUTime:    cpuTicks(time.Duration(step) * time.Millisecond),
				}}
				sampler.observe(service, tree, usageBase.Add(time.Duration(step)*time.Second))

				if step%10 == 0 {
					sampler.forget(service)
				}
			}
		})
	}

	wg.Wait()

	assert.NotPanics(t, func() {
		sampler.observe("gameapServer1", nil, usageBase.Add(time.Hour))
	})
}

func TestProcessTreeMetrics(t *testing.T) {
	got := processTreeMetrics(usageBase, "gameapServer5", processTreeUsage{
		CPUPercent:        142.5,
		HasCPU:            true,
		PrivateWorkingSet: 700 << 20,
		ReadBytes:         5300,
		WriteBytes:        830,
		Threads:           26,
	})

	want := []struct {
		name       string
		metricType domain.MetricType
		unit       domain.MetricUnit
		value      domain.MetricValue
	}{
		{metricServerCPUUsagePercent, domain.MetricTypeGauge, domain.MetricUnitPercent, domain.Float64Value(142.5)},
		{metricServerMemoryUsageBytes, domain.MetricTypeGauge, domain.MetricUnitBytes, domain.Uint64Value(700 << 20)},
		{metricServerBlockIOReadBytesTotal, domain.MetricTypeCounter, domain.MetricUnitBytes, domain.Uint64Value(5300)},
		{metricServerBlockIOWriteBytesTotal, domain.MetricTypeCounter, domain.MetricUnitBytes, domain.Uint64Value(830)},
		{metricServerProcessPIDs, domain.MetricTypeGauge, domain.MetricUnitCount, domain.Uint64Value(26)},
	}

	require.Len(t, got, len(want))

	for i, w := range want {
		assert.Equal(t, w.name, got[i].Name)
		assert.Equal(t, w.metricType, got[i].Type, w.name)
		assert.Equal(t, w.unit, got[i].Unit, w.name)
		assert.Equal(t, w.value, got[i].Value, w.name)
		assert.Equal(t, usageBase, got[i].Timestamp, w.name)
		assert.Equal(t, map[string]string{metricLabelService: "gameapServer5"}, got[i].Labels, w.name)
	}

	// The metrics collector adds the server labels to every metric in place.
	got[0].Labels["server_id"] = "5"
	assert.NotContains(t, got[1].Labels, "server_id")
}

func TestProcessTreeMetrics_WithoutCPU(t *testing.T) {
	got := processTreeMetrics(usageBase.Add(time.Minute), "gameapServer9", processTreeUsage{PrivateWorkingSet: 1 << 20})

	require.Len(t, got, 4)
	assert.Empty(t, collectByName(got, metricServerCPUUsagePercent))

	for _, name := range []string{
		metricServerMemoryLimitBytes,
		metricServerMemoryUsagePercent,
		metricServerNetworkReceiveBytesTotal,
		metricServerNetworkTransmitBytesTotal,
	} {
		assert.Empty(t, collectByName(got, name), name)
	}
}
