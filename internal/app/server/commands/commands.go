package commands

import (
	"context"
	"io"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/server/response"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/pkg/errors"
)

type Commands struct {
	executor contracts.Executor
}

func NewCommands(executor contracts.Executor) *Commands {
	return &Commands{
		executor: executor,
	}
}

func (c *Commands) Handle(ctx context.Context, readWriter io.ReadWriter) error {
	var msg commandExec
	decoder := decode.NewDecoder(readWriter)
	err := decoder.Decode(&msg)
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if err != nil {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Failed to decode message",
		})
	}

	return c.executeCommand(ctx, msg, readWriter)
}

func (c Commands) executeCommand(ctx context.Context, msg commandExec, writer io.Writer) error {
	logger.WithField(ctx, "command", msg.Command).Debug("Executing command")

	out, exitCode, err := c.executor.Exec(ctx, msg.Command, contracts.ExecutorOptions{
		WorkDir: msg.WorkDir,
	})

	if err != nil {
		logger.WithField(ctx, "error", err).Warn("Executing failed")

		return response.WriteResponse(writer, response.Response{
			Code: response.StatusError,
			Info: err.Error(),
		})
	}

	logger.Logger(ctx).
		WithField("command", msg.Command).
		WithField("exitCode", exitCode).
		WithField("outSize", len(out)).
		Debug("Command executed")

	return response.WriteResponse(writer, Response{
		Code:     response.StatusOK,
		ExitCode: exitCode,
		Output:   string(out),
	})
}
