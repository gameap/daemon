package metrics

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuffer_AppendCurrent_LatestPerSeries(t *testing.T) {
	buf := NewBuffer(10 * time.Minute)

	now := time.Now()

	buf.Append([]domain.Metric{
		{
			Name:      "gameap_node_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now.Add(-2 * time.Second),
			Value:     domain.Float64Value(10),
		},
		{
			Name:      "gameap_node_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now,
			Value:     domain.Float64Value(42),
		},
	})

	current := buf.Current()
	require.Len(t, current, 1)
	assert.Equal(t, "gameap_node_cpu_usage_percent", current[0].Name)
	assert.InDelta(t, 42.0, current[0].Value.Float64(), 0.0001)
	assert.Equal(t, now, current[0].Timestamp)
}

func TestBuffer_LabelsDistinguishSeries(t *testing.T) {
	buf := NewBuffer(time.Minute)
	now := time.Now()

	buf.Append([]domain.Metric{
		{
			Name:      "gameap_node_disk_usage_bytes",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Labels:    map[string]string{"mount": "/"},
			Timestamp: now,
			Value:     domain.Uint64Value(100),
		},
		{
			Name:      "gameap_node_disk_usage_bytes",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitBytes,
			Labels:    map[string]string{"mount": "/var"},
			Timestamp: now,
			Value:     domain.Uint64Value(200),
		},
	})

	current := buf.Current()
	assert.Len(t, current, 2)

	values := map[string]uint64{}
	for _, m := range current {
		values[m.Labels["mount"]] = m.Value.Uint64()
	}
	assert.Equal(t, uint64(100), values["/"])
	assert.Equal(t, uint64(200), values["/var"])
}

func TestBuffer_HistoryEvictsOlderThanRetention(t *testing.T) {
	buf := NewBuffer(5 * time.Second)
	now := time.Now()

	buf.Append([]domain.Metric{{
		Name:      "gameap_node_cpu_usage_percent",
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitPercent,
		Timestamp: now.Add(-10 * time.Second),
		Value:     domain.Float64Value(1),
	}})
	buf.Append([]domain.Metric{{
		Name:      "gameap_node_cpu_usage_percent",
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitPercent,
		Timestamp: now,
		Value:     domain.Float64Value(2),
	}})

	history, actual := buf.History(time.Minute)
	require.Len(t, history, 1, "older sample must be evicted")
	assert.InDelta(t, 2.0, history[0].Value.Float64(), 0.0001)
	assert.Equal(t, 5*time.Second, actual, "actual window must be capped at retention")
}

func TestBuffer_HistoryReturnsAllPointsInWindow(t *testing.T) {
	buf := NewBuffer(time.Minute)
	now := time.Now()

	for i := 0; i < 4; i++ {
		buf.Append([]domain.Metric{{
			Name:      "gameap_node_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now.Add(time.Duration(-i) * time.Second),
			Value:     domain.Float64Value(float64(i)),
		}})
	}

	history, actual := buf.History(2500 * time.Millisecond)

	assert.Equal(t, 2500*time.Millisecond, actual)
	assert.Len(t, history, 3, "window covers t-0, t-1, t-2 (with margin for History's time.Now)")
}

func TestBuffer_AcceptsZeroValuesViaConstructors(t *testing.T) {
	buf := NewBuffer(time.Minute)
	now := time.Now()

	buf.Append([]domain.Metric{
		{
			Name:      "gameap_server_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: now,
			Value:     domain.Float64Value(0),
		},
		{
			Name:      "gameap_server_process_pids",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitCount,
			Timestamp: now,
			Value:     domain.Uint64Value(0),
		},
	})

	current := buf.Current()
	require.Len(t, current, 2, "explicit zero values must be retained, only truly unset ones are skipped")
	for _, m := range current {
		assert.True(t, m.Value.IsSet())
	}
}

func TestBuffer_EmptyValueIsSkipped(t *testing.T) {
	buf := NewBuffer(time.Minute)

	buf.Append([]domain.Metric{{
		Name:      "gameap_node_cpu_usage_percent",
		Type:      domain.MetricTypeGauge,
		Unit:      domain.MetricUnitPercent,
		Timestamp: time.Now(),
		// Value left zero/unset on purpose.
	}})

	assert.Empty(t, buf.Current())
}
