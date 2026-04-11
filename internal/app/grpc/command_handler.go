package grpc

import (
	"context"

	"github.com/gameap/daemon/internal/app/contracts"
	pb "github.com/gameap/gameap/pkg/proto"
	"github.com/pkg/errors"
)

type GRPCCommandHandler struct {
	executor contracts.Executor
	workDir  string
}

func NewGRPCCommandHandler(executor contracts.Executor, workDir string) *GRPCCommandHandler {
	return &GRPCCommandHandler{
		executor: executor,
		workDir:  workDir,
	}
}

func (h *GRPCCommandHandler) HandleCommand(
	ctx context.Context, requestID string, cmd *pb.CommandRequest,
) (*pb.CommandResult, error) {
	workDir := h.workDir
	if cmd.WorkDir != "" {
		workDir = cmd.WorkDir
	}

	output, exitCode, err := h.executor.Exec(ctx, cmd.Command, contracts.ExecutorOptions{
		WorkDir: workDir,
	})
	if err != nil {
		return &pb.CommandResult{
			RequestId: requestID,
			CommandId: cmd.CommandId,
			ExitCode:  int32(exitCode),
			Output:    []byte(errors.Wrap(err, "command execution failed").Error()),
			Error:     err.Error(),
		}, nil
	}

	return &pb.CommandResult{
		RequestId: requestID,
		CommandId: cmd.CommandId,
		ExitCode:  int32(exitCode),
		Output:    output,
	}, nil
}
