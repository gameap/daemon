package grpc

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
)

const (
	defaultBatchSize     = 50
	defaultFlushInterval = 5 * time.Second
)

type StatusSender interface {
	SendServerStatuses(statuses []*pb.ServerStatus)
}

type ServerStatusReporter struct {
	sender        StatusSender
	batchSize     int
	flushInterval time.Duration

	mu       sync.Mutex
	buffer   []*pb.ServerStatus
	shutdown chan struct{}
	wg       sync.WaitGroup
}

func NewServerStatusReporter(sender StatusSender) *ServerStatusReporter {
	return &ServerStatusReporter{
		sender:        sender,
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
		buffer:        make([]*pb.ServerStatus, 0, defaultBatchSize),
		shutdown:      make(chan struct{}),
	}
}

func (r *ServerStatusReporter) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.flushLoop(ctx)
}

func (r *ServerStatusReporter) Stop() {
	close(r.shutdown)
	r.wg.Wait()
	r.flush()
}

func (r *ServerStatusReporter) Report(server *domain.Server) {
	status := DomainServerToProtoStatus(server)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.buffer = append(r.buffer, status)

	if len(r.buffer) >= r.batchSize {
		r.flushLocked()
	}
}

func (r *ServerStatusReporter) flushLoop(ctx context.Context) {
	defer r.wg.Done()

	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.shutdown:
			return
		case <-ticker.C:
			r.flush()
		}
	}
}

func (r *ServerStatusReporter) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

func (r *ServerStatusReporter) flushLocked() {
	if len(r.buffer) == 0 {
		return
	}

	statuses := make([]*pb.ServerStatus, len(r.buffer))
	copy(statuses, r.buffer)
	r.buffer = r.buffer[:0]

	r.sender.SendServerStatuses(statuses)

	log.WithField("count", len(statuses)).Debug("Flushed server statuses")
}
