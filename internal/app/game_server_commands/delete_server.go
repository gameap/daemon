package game_server_commands

import (
	"context"
	"os"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
)

type deleteServer struct {
	baseCommand
	bufCommand
}

func newDeleteServer(cfg *config.Config) *deleteServer {
	return &deleteServer{
		baseCommand{
			cfg:      cfg,
			complete: false,
			result:   UnknownResult,
		},
		bufCommand{output: components.NewSafeBuffer()},
	}
}

func (cmd *deleteServer) Execute(ctx context.Context, server *domain.Server) error {
	defer func() {
		cmd.complete = true
	}()

	path := makeFullServerPath(cmd.cfg, server.Dir())

	if cmd.cfg.Scripts.Delete == "" {
		err := os.RemoveAll(path)
		if err != nil {
			cmd.result = ErrorResult
			_, _ = cmd.output.Write([]byte(err.Error()))
			return err
		}

		return nil
	}

	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Status, "")

	var err error
	cmd.result, err = components.ExecWithWriter(ctx, command, cmd.output, components.ExecutorOptions{
		WorkDir: path,
	})
	if err != nil {
		return err
	}

	return nil
}
