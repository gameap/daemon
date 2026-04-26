package metrics

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func metricTypeToProto(t domain.MetricType) pb.MetricType {
	switch t {
	case domain.MetricTypeGauge:
		return pb.MetricType_METRIC_TYPE_GAUGE
	case domain.MetricTypeCounter:
		return pb.MetricType_METRIC_TYPE_COUNTER
	default:
		return pb.MetricType_METRIC_TYPE_UNSPECIFIED
	}
}

func metricUnitToProto(u domain.MetricUnit) pb.MetricUnit {
	switch u {
	case domain.MetricUnitCount:
		return pb.MetricUnit_METRIC_UNIT_COUNT
	case domain.MetricUnitPercent:
		return pb.MetricUnit_METRIC_UNIT_PERCENT
	case domain.MetricUnitRatio:
		return pb.MetricUnit_METRIC_UNIT_RATIO
	case domain.MetricUnitBytes:
		return pb.MetricUnit_METRIC_UNIT_BYTES
	case domain.MetricUnitBits:
		return pb.MetricUnit_METRIC_UNIT_BITS
	case domain.MetricUnitSeconds:
		return pb.MetricUnit_METRIC_UNIT_SECONDS
	case domain.MetricUnitMilliseconds:
		return pb.MetricUnit_METRIC_UNIT_MILLISECONDS
	case domain.MetricUnitMicroseconds:
		return pb.MetricUnit_METRIC_UNIT_MICROSECONDS
	case domain.MetricUnitNanoseconds:
		return pb.MetricUnit_METRIC_UNIT_NANOSECONDS
	case domain.MetricUnitHertz:
		return pb.MetricUnit_METRIC_UNIT_HERTZ
	case domain.MetricUnitCelsius:
		return pb.MetricUnit_METRIC_UNIT_CELSIUS
	case domain.MetricUnitWatts:
		return pb.MetricUnit_METRIC_UNIT_WATTS
	case domain.MetricUnitVolts:
		return pb.MetricUnit_METRIC_UNIT_VOLTS
	case domain.MetricUnitRPM:
		return pb.MetricUnit_METRIC_UNIT_RPM
	default:
		return pb.MetricUnit_METRIC_UNIT_UNSPECIFIED
	}
}

// setMetricPointValue assigns the matching oneof variant on point. Done via
// helper rather than returning the interface because the proto-generated
// isMetricPoint_Value interface is unexported.
func setMetricPointValue(point *pb.MetricPoint, v domain.MetricValue) {
	switch {
	case v.IsFloat64():
		point.Value = &pb.MetricPoint_DoubleValue{DoubleValue: v.Float64()}
	case v.IsUint64():
		point.Value = &pb.MetricPoint_UintValue{UintValue: v.Uint64()}
	case v.IsInt64():
		point.Value = &pb.MetricPoint_IntValue{IntValue: v.Int64()}
	default:
		point.Value = &pb.MetricPoint_DoubleValue{DoubleValue: 0}
	}
}

// ToMetricsResponse groups the given samples into pb.MetricSeries by
// (Name, Labels), preserving point order, and stamps the response.
//
// actualWindow is the actual time window covered by the samples (in seconds);
// pass 0 for current snapshots.
func ToMetricsResponse(samples []domain.Metric, actualWindow time.Duration) *pb.MetricsResponse {
	resp := &pb.MetricsResponse{
		Timestamp: timestamppb.Now(),
	}
	if actualWindow > 0 {
		resp.ActualWindowSeconds = uint32(actualWindow.Seconds())
	}

	if len(samples) == 0 {
		return resp
	}

	// Preserve first-seen order of series so output is stable.
	seriesOrder := make([]string, 0)
	seriesByKey := make(map[string]*pb.MetricSeries)

	for i := range samples {
		s := samples[i]

		key := s.SeriesKey()
		series, ok := seriesByKey[key]
		if !ok {
			series = &pb.MetricSeries{
				Name:   s.Name,
				Type:   metricTypeToProto(s.Type),
				Unit:   metricUnitToProto(s.Unit),
				Labels: cloneLabels(s.Labels),
			}
			seriesByKey[key] = series
			seriesOrder = append(seriesOrder, key)
		}

		point := &pb.MetricPoint{
			Timestamp: timestamppb.New(s.Timestamp),
		}
		setMetricPointValue(point, s.Value)
		series.Points = append(series.Points, point)
	}

	resp.Series = make([]*pb.MetricSeries, 0, len(seriesOrder))
	for _, k := range seriesOrder {
		resp.Series = append(resp.Series, seriesByKey[k])
	}

	return resp
}
