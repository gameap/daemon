package domain

import (
	"sort"
	"strings"
	"time"
)

type MetricType uint8

const (
	MetricTypeUnspecified MetricType = 0
	MetricTypeGauge       MetricType = 1
	MetricTypeCounter     MetricType = 2
)

type MetricUnit uint8

const (
	MetricUnitUnspecified  MetricUnit = 0
	MetricUnitCount        MetricUnit = 1
	MetricUnitPercent      MetricUnit = 2
	MetricUnitRatio        MetricUnit = 3
	MetricUnitBytes        MetricUnit = 10
	MetricUnitBits         MetricUnit = 11
	MetricUnitSeconds      MetricUnit = 20
	MetricUnitMilliseconds MetricUnit = 21
	MetricUnitMicroseconds MetricUnit = 22
	MetricUnitNanoseconds  MetricUnit = 23
	MetricUnitHertz        MetricUnit = 30
	MetricUnitCelsius      MetricUnit = 40
	MetricUnitWatts        MetricUnit = 41
	MetricUnitVolts        MetricUnit = 42
	MetricUnitRPM          MetricUnit = 43
)

type valueKind uint8

const (
	valueKindUnset valueKind = iota
	valueKindFloat64
	valueKindUint64
	valueKindInt64
)

// MetricValue is a sum-type holding exactly one of float64/uint64/int64.
// Use Float64Value/Uint64Value/Int64Value to construct it. The zero value
// is "unset" and reports Kind() == 0.
type MetricValue struct {
	kind valueKind
	f    float64
	u    uint64
	i    int64
}

func Float64Value(v float64) MetricValue {
	return MetricValue{kind: valueKindFloat64, f: v}
}

func Uint64Value(v uint64) MetricValue {
	return MetricValue{kind: valueKindUint64, u: v}
}

func Int64Value(v int64) MetricValue {
	return MetricValue{kind: valueKindInt64, i: v}
}

func (v MetricValue) IsFloat64() bool { return v.kind == valueKindFloat64 }
func (v MetricValue) IsUint64() bool  { return v.kind == valueKindUint64 }
func (v MetricValue) IsInt64() bool   { return v.kind == valueKindInt64 }
func (v MetricValue) IsSet() bool     { return v.kind != valueKindUnset }

func (v MetricValue) Float64() float64 { return v.f }
func (v MetricValue) Uint64() uint64   { return v.u }
func (v MetricValue) Int64() int64     { return v.i }

// AsFloat64 returns the value normalized to float64 regardless of underlying kind.
// Useful for ad-hoc aggregation; precision capped at 2^53 per proto contract.
func (v MetricValue) AsFloat64() float64 {
	switch v.kind {
	case valueKindFloat64:
		return v.f
	case valueKindUint64:
		return float64(v.u)
	case valueKindInt64:
		return float64(v.i)
	default:
		return 0
	}
}

// Metric is a single labeled sample. Maps 1:1 onto pb.MetricSeries (one point)
// — the metrics package groups multiple Metrics into pb.MetricSeries by
// (Name, Labels) when serialising to a MetricsResponse.
type Metric struct {
	Name      string
	Type      MetricType
	Unit      MetricUnit
	Labels    map[string]string
	Timestamp time.Time
	Value     MetricValue
}

// SeriesKey builds a deterministic identifier for the (Name, Labels) tuple.
// Label keys are sorted so two metrics with semantically identical labels
// always share a key regardless of map iteration order.
func (m Metric) SeriesKey() string {
	if len(m.Labels) == 0 {
		return m.Name
	}

	keys := make([]string, 0, len(m.Labels))
	for k := range m.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.Grow(len(m.Name) + 1 + len(keys)*16)
	b.WriteString(m.Name)
	b.WriteByte('|')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m.Labels[k])
	}

	return b.String()
}
