package metrics

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
)

// Collector produces a snapshot of metrics on demand.
type Collector interface {
	Collect(ctx context.Context) ([]domain.Metric, error)
}

// Service runs collectors on a tick, accumulates samples in the in-memory
// ring buffer, and answers Current()/History() lookups for the gRPC handler.
type Service struct {
	buffer   *Buffer
	interval time.Duration

	collectorsMu sync.RWMutex
	collectors   []Collector
}

func NewService(buffer *Buffer, interval time.Duration, collectors ...Collector) *Service {
	return &Service{
		buffer:     buffer,
		interval:   interval,
		collectors: collectors,
	}
}

// AddCollector appends a collector after construction. Useful when a collector
// depends on a component built later in the DI lifecycle.
func (s *Service) AddCollector(c Collector) {
	s.collectorsMu.Lock()
	defer s.collectorsMu.Unlock()
	s.collectors = append(s.collectors, c)
}

// Run blocks until ctx is cancelled, ticking every s.interval and persisting
// each tick's samples to the buffer. Errors from individual collectors are
// logged; one failing collector does not skip the rest.
func (s *Service) Run(ctx context.Context) error {
	if s.interval <= 0 {
		return nil
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	s.collectorsMu.RLock()
	collectors := make([]Collector, len(s.collectors))
	copy(collectors, s.collectors)
	s.collectorsMu.RUnlock()

	all := make([]domain.Metric, 0, 64)
	for _, c := range collectors {
		samples, err := c.Collect(ctx)
		if err != nil {
			log.WithError(err).Warn("metrics collector failed")
		}
		if len(samples) > 0 {
			all = append(all, samples...)
		}
	}

	if len(all) == 0 {
		return
	}
	s.buffer.Append(all)
}

func (s *Service) Current() *pb.MetricsResponse {
	return ToMetricsResponse(s.buffer.Current(), 0)
}

func (s *Service) History(window time.Duration) *pb.MetricsResponse {
	samples, actual := s.buffer.History(window)
	return ToMetricsResponse(samples, actual)
}

// Buffer exposes the underlying ring buffer for tests and internal callers.
func (s *Service) Buffer() *Buffer {
	return s.buffer
}
