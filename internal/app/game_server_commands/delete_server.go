package gameservercommands

import (
	"context"
	"os"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

type deleteServer struct {
	baseCommand
	bufCommand
}

func newDeleteServer(cfg *config.Config, executor interfaces.Executor) *deleteServer {
	return &deleteServer{
		baseCommand{
			cfg:      cfg,
			executor: executor,
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

	if cmd.cfg.Scripts.Delete != "" {
		return cmd.removeByScript(ctx, server)
	}

	return cmd.removeByFilesystem(ctx, server)
}

func (cmd *deleteServer) removeByScript(ctx context.Context, server *domain.Server) error {
	path := makeFullServerPath(cmd.cfg, server.Dir())

	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Status, "")

	var err error
	cmd.result, err = cmd.executor.ExecWithWriter(ctx, command, cmd.output, components.ExecutorOptions{
		WorkDir: path,
	})

	if err != nil {
		cmd.result = ErrorResult
		_, _ = cmd.output.Write([]byte(err.Error()))
		return err
	}

	return err
}

func (cmd *deleteServer) removeByFilesystem(_ context.Context, server *domain.Server) error {
	path := makeFullServerPath(cmd.cfg, server.Dir())

	err := os.RemoveAll(path)
	if err != nil {
		cmd.result = ErrorResult
		_, _ = cmd.output.Write([]byte(err.Error()))
		return err
	}

	return nil
}
