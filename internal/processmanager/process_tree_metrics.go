package processmanager

import (
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

const (
	filetimeTick = 100 * time.Nanosecond

	// filetimeUnixEpoch is the Unix epoch in FILETIME ticks, which count from 1601-01-01 UTC.
	filetimeUnixEpoch = 116444736000000000
)

// filetimeTicks converts t to the scale process creation times use.
func filetimeTicks(t time.Time) int64 {
	return t.UnixNano()/int64(filetimeTick) + filetimeUnixEpoch
}

// processKey identifies one process for its whole life. The ID alone does not: Windows hands a
// freed ID to the next process that starts.
type processKey struct {
	pid        uint32
	createTime int64
}

type processCounters struct {
	cpuTime    int64
	readBytes  uint64
	writeBytes uint64
}

// processTreeSample is what one observation of a service's processes leaves for the next one: the
// counters of every process, and the I/O totals reported so far.
type processTreeSample struct {
	at         time.Time
	counters   map[processKey]processCounters
	readBytes  uint64
	writeBytes uint64
}

// processTreeUsage is what the processes of a service used, in the units the metrics report.
type processTreeUsage struct {
	CPUPercent        float64
	HasCPU            bool
	PrivateWorkingSet uint64
	ReadBytes         uint64
	WriteBytes        uint64
	Threads           uint64
}

// computeProcessTreeUsage measures tree against the previous observation of the same service and
// returns the sample the next observation is measured against.
//
// CPU time and I/O are counted per process, so a process that exits or starts between two
// observations neither pulls the totals down nor counts twice:
//   - a process seen last time contributes what it used since then;
//   - a process started after the last observation contributes everything it used, all of which
//     falls into the interval;
//   - any other process has no baseline yet and contributes from the next observation on.
//
// The I/O totals therefore only grow while the service keeps running, starting from what its
// processes report when it is first observed. CPU needs an interval, so the first observation
// reports none. What a process used between the last observation and its exit is lost.
func computeProcessTreeUsage(
	prior *processTreeSample, tree []processInfo, at time.Time,
) (processTreeUsage, processTreeSample) {
	usage := processTreeUsage{}
	next := processTreeSample{
		at:       at,
		counters: make(map[processKey]processCounters, len(tree)),
	}

	var priorTicks int64
	if prior != nil {
		priorTicks = filetimeTicks(prior.at)
	}

	var cpuTicks int64
	var readBytes, writeBytes uint64

	for _, p := range tree {
		key := processKey{pid: p.PID, createTime: p.CreateTime}
		current := processCounters{cpuTime: p.CPUTime, readBytes: p.ReadBytes, writeBytes: p.WriteBytes}
		next.counters[key] = current

		usage.PrivateWorkingSet += p.PrivateWorkingSet
		usage.Threads += uint64(p.Threads)

		if prior == nil {
			readBytes += current.readBytes
			writeBytes += current.writeBytes

			continue
		}

		before, seen := prior.counters[key]

		switch {
		case seen:
			cpuTicks += max(current.cpuTime-before.cpuTime, 0)
			readBytes += counterGrowth(before.readBytes, current.readBytes)
			writeBytes += counterGrowth(before.writeBytes, current.writeBytes)
		case p.CreateTime >= priorTicks:
			cpuTicks += current.cpuTime
			readBytes += current.readBytes
			writeBytes += current.writeBytes
		}
	}

	next.readBytes, next.writeBytes = readBytes, writeBytes

	if prior != nil {
		next.readBytes += prior.readBytes
		next.writeBytes += prior.writeBytes

		if wall := at.Sub(prior.at); wall > 0 {
			usage.CPUPercent = float64(time.Duration(cpuTicks)*filetimeTick) / float64(wall) * 100
			usage.HasCPU = true
		}
	}

	usage.ReadBytes, usage.WriteBytes = next.readBytes, next.writeBytes

	return usage, next
}

func counterGrowth(before, after uint64) uint64 {
	if after < before {
		return 0
	}

	return after - before
}

// processTreeSampler keeps the last observation of every service, so that each observation is
// measured against the one before it.
type processTreeSampler struct {
	mu      sync.Mutex
	samples map[string]processTreeSample
}

func newProcessTreeSampler() *processTreeSampler {
	return &processTreeSampler{samples: make(map[string]processTreeSample)}
}

// observe measures the processes of a service and keeps them as the baseline of the next observation.
func (s *processTreeSampler) observe(service string, tree []processInfo, at time.Time) processTreeUsage {
	s.mu.Lock()
	defer s.mu.Unlock()

	var prior *processTreeSample
	if sample, ok := s.samples[service]; ok {
		prior = &sample
	}

	usage, next := computeProcessTreeUsage(prior, tree, at)
	s.samples[service] = next

	return usage
}

// forget drops the baseline of a service whose processes are gone, so the service is measured from
// scratch once it runs again instead of against processes that no longer exist.
func (s *processTreeSampler) forget(service string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.samples, service)
}

// processTreeMetrics reports the usage of a service's processes as per-server metrics. There is no
// memory limit to compare against, and Windows keeps no per-process network counters.
func processTreeMetrics(ts time.Time, service string, usage processTreeUsage) []domain.Metric {
	metric := func(
		name string, metricType domain.MetricType, unit domain.MetricUnit, value domain.MetricValue,
	) domain.Metric {
		return domain.Metric{
			Name:      name,
			Type:      metricType,
			Unit:      unit,
			Labels:    map[string]string{metricLabelService: service},
			Timestamp: ts,
			Value:     value,
		}
	}

	out := make([]domain.Metric, 0, 5)

	if usage.HasCPU {
		out = append(out, metric(
			metricServerCPUUsagePercent, domain.MetricTypeGauge, domain.MetricUnitPercent,
			domain.Float64Value(usage.CPUPercent),
		))
	}

	return append(out,
		metric(
			metricServerMemoryUsageBytes, domain.MetricTypeGauge, domain.MetricUnitBytes,
			domain.Uint64Value(usage.PrivateWorkingSet),
		),
		metric(
			metricServerBlockIOReadBytesTotal, domain.MetricTypeCounter, domain.MetricUnitBytes,
			domain.Uint64Value(usage.ReadBytes),
		),
		metric(
			metricServerBlockIOWriteBytesTotal, domain.MetricTypeCounter, domain.MetricUnitBytes,
			domain.Uint64Value(usage.WriteBytes),
		),
		metric(
			metricServerProcessPIDs, domain.MetricTypeGauge, domain.MetricUnitCount,
			domain.Uint64Value(usage.Threads),
		),
	)
}
