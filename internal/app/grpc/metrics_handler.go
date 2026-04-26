package grpc

import (
	"context"
	"time"

	pb "github.com/gameap/gameap/pkg/proto"
)

// MetricsProvider is implemented by metrics.Service. The handler depends on
// the small interface to avoid an import cycle with the metrics package.
type MetricsProvider interface {
	Current() *pb.MetricsResponse
	History(window time.Duration) *pb.MetricsResponse
}

type GRPCMetricsHandler struct {
	provider MetricsProvider
}

func NewGRPCMetricsHandler(provider MetricsProvider) *GRPCMetricsHandler {
	return &GRPCMetricsHandler{provider: provider}
}

// HandleMetricsRequest answers a pull-mode MetricsRequest with either the
// most recent snapshot of all series (CurrentMetricsRequest) or every retained
// point inside the requested window (MetricsHistoryRequest).
func (h *GRPCMetricsHandler) HandleMetricsRequest(
	_ context.Context, _ string, req *pb.MetricsRequest,
) *pb.MetricsResponse {
	if req == nil {
		return h.provider.Current()
	}

	switch kind := req.GetKind().(type) {
	case *pb.MetricsRequest_History:
		seconds := time.Duration(0)
		if kind.History != nil {
			seconds = time.Duration(kind.History.GetSeconds()) * time.Second
		}
		return h.provider.History(seconds)
	case *pb.MetricsRequest_Current:
		return h.provider.Current()
	default:
		return h.provider.Current()
	}
}
