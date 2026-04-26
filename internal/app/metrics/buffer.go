package metrics

import (
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

// seriesPoint is one timestamped value for a series. It holds only the
// per-point fields; identity (name/type/unit/labels) is shared by the
// enclosing seriesRing.
type seriesPoint struct {
	ts    time.Time
	value domain.MetricValue
}

// seriesRing stores all retained points for a single (name, labels) tuple.
// Points are appended in monotonically non-decreasing timestamp order; older
// points are dropped on Append once they fall outside the retention window.
type seriesRing struct {
	name   string
	typ    domain.MetricType
	unit   domain.MetricUnit
	labels map[string]string
	points []seriesPoint
}

func (r *seriesRing) appendPoint(ts time.Time, v domain.MetricValue, retention time.Duration) {
	r.points = append(r.points, seriesPoint{ts: ts, value: v})

	if retention <= 0 {
		return
	}
	cutoff := ts.Add(-retention)

	// Most appends evict 0–1 stale points, so a linear scan from the head
	// beats binary search on the typical hot path.
	drop := 0
	for drop < len(r.points) && r.points[drop].ts.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		r.points = append(r.points[:0], r.points[drop:]...)
	}
}

// Buffer is an in-memory ring buffer of metric series indexed by SeriesKey.
// Safe for concurrent use.
type Buffer struct {
	mu        sync.RWMutex
	retention time.Duration
	series    map[string]*seriesRing
}

func NewBuffer(retention time.Duration) *Buffer {
	return &Buffer{
		retention: retention,
		series:    make(map[string]*seriesRing),
	}
}

func (b *Buffer) Retention() time.Duration {
	return b.retention
}

// Append stores the given metrics. New series are created on first sight;
// existing series have their identity refreshed (labels can be updated when
// upstream changes them, e.g. PID changes after a restart).
func (b *Buffer) Append(ms []domain.Metric) {
	if len(ms) == 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for i := range ms {
		m := ms[i]
		if !m.Value.IsSet() {
			continue
		}

		key := m.SeriesKey()
		ring, ok := b.series[key]
		if !ok {
			ring = &seriesRing{
				name:   m.Name,
				typ:    m.Type,
				unit:   m.Unit,
				labels: cloneLabels(m.Labels),
			}
			b.series[key] = ring
		} else {
			ring.typ = m.Type
			ring.unit = m.Unit
		}

		ring.appendPoint(m.Timestamp, m.Value, b.retention)
	}
}

// Current returns the most recent point of every non-empty series.
// Empty series (all points evicted) are skipped.
func (b *Buffer) Current() []domain.Metric {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]domain.Metric, 0, len(b.series))
	for _, ring := range b.series {
		if len(ring.points) == 0 {
			continue
		}
		last := ring.points[len(ring.points)-1]
		out = append(out, domain.Metric{
			Name:      ring.name,
			Type:      ring.typ,
			Unit:      ring.unit,
			Labels:    cloneLabels(ring.labels),
			Timestamp: last.ts,
			Value:     last.value,
		})
	}

	return out
}

// History returns every retained point newer than now-window across all
// series, plus the actual covered window (capped by retention). The caller
// should pass actualWindow into MetricsResponse.actual_window_seconds.
func (b *Buffer) History(window time.Duration) (samples []domain.Metric, actualWindow time.Duration) {
	if window <= 0 {
		window = b.retention
	}
	if window > b.retention {
		window = b.retention
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	now := time.Now()
	cutoff := now.Add(-window)

	out := make([]domain.Metric, 0)
	for _, ring := range b.series {
		for _, p := range ring.points {
			if p.ts.Before(cutoff) {
				continue
			}
			out = append(out, domain.Metric{
				Name:      ring.name,
				Type:      ring.typ,
				Unit:      ring.unit,
				Labels:    cloneLabels(ring.labels),
				Timestamp: p.ts,
				Value:     p.value,
			})
		}
	}

	return out, window
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
