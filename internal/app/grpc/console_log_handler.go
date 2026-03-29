package grpc

import (
	"bytes"
	"context"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	pb "github.com/gameap/gameap/pkg/proto"
	log "github.com/sirupsen/logrus"
)

type GRPCConsoleLogHandler struct {
	serverRepo     domain.ServerRepository
	processManager contracts.ProcessManager
}

func NewGRPCConsoleLogHandler(
	serverRepo domain.ServerRepository,
	processManager contracts.ProcessManager,
) *GRPCConsoleLogHandler {
	return &GRPCConsoleLogHandler{
		serverRepo:     serverRepo,
		processManager: processManager,
	}
}

func (h *GRPCConsoleLogHandler) HandleConsoleLogRequest(
	ctx context.Context, requestID string, req *pb.ConsoleLogRequest,
) (*pb.ConsoleLogResponse, error) {
	serverID := req.GetServerId()

	logEntry := log.WithFields(log.Fields{
		"request_id": requestID,
		"server_id":  serverID,
	})

	server, err := h.serverRepo.FindByID(ctx, int(serverID))
	if err != nil {
		logEntry.WithError(err).Error("Failed to find server for console log")
		return &pb.ConsoleLogResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "failed to find server",
		}, nil
	}
	if server == nil {
		logEntry.Warn("Server not found for console log")
		return &pb.ConsoleLogResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "server not found",
		}, nil
	}

	var buf bytes.Buffer
	_, err = h.processManager.GetOutput(ctx, server, &buf)
	if err != nil {
		logEntry.WithError(err).Error("Failed to get console output")
		return &pb.ConsoleLogResponse{
			RequestId: requestID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	data := buf.Bytes()
	if maxBytes := req.GetMaxBytes(); maxBytes > 0 && int64(len(data)) > maxBytes {
		data = data[int64(len(data))-maxBytes:]
	}

	return &pb.ConsoleLogResponse{
		RequestId: requestID,
		Success:   true,
		Data:      data,
	}, nil
}
