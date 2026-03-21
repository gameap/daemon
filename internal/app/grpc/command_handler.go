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

func (h *GRPCCommandHandler) HandleCommand(ctx context.Context, cmd *pb.CommandRequest) (*pb.CommandResult, error) {
	output, exitCode, err := h.executor.Exec(ctx, cmd.Command, contracts.ExecutorOptions{
		WorkDir: h.workDir,
	})
	if err != nil {
		return &pb.CommandResult{
			CommandId: cmd.CommandId,
			ExitCode:  int32(exitCode),
			Output:    []byte(errors.Wrap(err, "command execution failed").Error()),
			Error:     err.Error(),
		}, nil
	}

	return &pb.CommandResult{
		CommandId: cmd.CommandId,
		ExitCode:  int32(exitCode),
		Output:    output,
	}, nil
}
