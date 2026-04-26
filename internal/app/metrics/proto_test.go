package metrics

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToMetricsResponse_GroupsBySeries(t *testing.T) {
	now := time.Now()
	samples := []domain.Metric{
		{
			Name:      "gameap_server_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Labels:    map[string]string{"server_id": "1"},
			Timestamp: now,
			Value:     domain.Float64Value(50),
		},
		{
			Name:      "gameap_server_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Labels:    map[string]string{"server_id": "1"},
			Timestamp: now.Add(time.Second),
			Value:     domain.Float64Value(60),
		},
		{
			Name:      "gameap_server_cpu_usage_percent",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Labels:    map[string]string{"server_id": "2"},
			Timestamp: now,
			Value:     domain.Float64Value(10),
		},
	}

	resp := ToMetricsResponse(samples, 0)

	require.NotNil(t, resp)
	require.Len(t, resp.Series, 2)
	assert.Equal(t, uint32(0), resp.ActualWindowSeconds)

	pointsByServer := map[string]int{}
	for _, s := range resp.Series {
		pointsByServer[s.Labels["server_id"]] = len(s.Points)
	}
	assert.Equal(t, 2, pointsByServer["1"])
	assert.Equal(t, 1, pointsByServer["2"])
}

func TestToMetricsResponse_PreservesValueKinds(t *testing.T) {
	samples := []domain.Metric{
		{
			Name:      "double",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitPercent,
			Timestamp: time.Now(),
			Value:     domain.Float64Value(3.14),
		},
		{
			Name:      "uint",
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Timestamp: time.Now(),
			Value:     domain.Uint64Value(42),
		},
		{
			Name:      "int",
			Type:      domain.MetricTypeGauge,
			Unit:      domain.MetricUnitCount,
			Timestamp: time.Now(),
			Value:     domain.Int64Value(-7),
		},
	}

	resp := ToMetricsResponse(samples, 0)

	byName := map[string]*pb.MetricSeries{}
	for _, s := range resp.Series {
		byName[s.Name] = s
	}

	require.Contains(t, byName, "double")
	_, ok := byName["double"].Points[0].Value.(*pb.MetricPoint_DoubleValue)
	assert.True(t, ok, "double series must use double_value oneof")

	require.Contains(t, byName, "uint")
	_, ok = byName["uint"].Points[0].Value.(*pb.MetricPoint_UintValue)
	assert.True(t, ok, "uint series must use uint_value oneof")

	require.Contains(t, byName, "int")
	_, ok = byName["int"].Points[0].Value.(*pb.MetricPoint_IntValue)
	assert.True(t, ok, "int series must use int_value oneof")
}

func TestToMetricsResponse_MapsTypeAndUnit(t *testing.T) {
	resp := ToMetricsResponse([]domain.Metric{
		{
			Name:      "x",
			Type:      domain.MetricTypeCounter,
			Unit:      domain.MetricUnitBytes,
			Timestamp: time.Now(),
			Value:     domain.Uint64Value(1),
		},
	}, 0)
	require.Len(t, resp.Series, 1)
	assert.Equal(t, pb.MetricType_METRIC_TYPE_COUNTER, resp.Series[0].Type)
	assert.Equal(t, pb.MetricUnit_METRIC_UNIT_BYTES, resp.Series[0].Unit)
}

func TestToMetricsResponse_ActualWindow(t *testing.T) {
	resp := ToMetricsResponse(nil, 90*time.Second)
	assert.Equal(t, uint32(90), resp.ActualWindowSeconds)
}
