package commands

import (
	"context"
	"io"

	"github.com/et-nik/binngo/decode"
	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/server/response"
)

type Commands struct {}

func NewCommands() *Commands {
	return &Commands{}
}

func (c *Commands) Handle(ctx context.Context, readWriter io.ReadWriter) error {
	var msg commandExec
	decoder := decode.NewDecoder(readWriter)
	err := decoder.Decode(&msg)
	if err != nil {
		return response.WriteResponse(readWriter, response.Response{
			Code: response.StatusError,
			Info: "Failed to decode message",
		})
	}

	return c.executeCommand(ctx, msg, readWriter)
}

func (c Commands) executeCommand(ctx context.Context, msg commandExec, writer io.Writer) error {
	out, exitCode, err := components.Exec(ctx, msg.Command, components.ExecutorOptions{
		WorkDir: msg.WorkDir,
	})

	if err != nil {
		return response.WriteResponse(writer, response.Response{
			Code: response.StatusError,
			Info: err.Error(),
		})
	}

	return response.WriteResponse(writer, Response{
		Code: response.StatusOK,
		ExitCode: exitCode,
		Output: string(out),
	})
}
