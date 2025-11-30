package gameservercommands

import (
	"context"
	"os"
	"path/filepath"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

var errForbiddenWorkDirectoryPath = errors.New("forbidden game server work directory path")

type defaultDeleteServer struct {
	bufCommand
	baseCommand
}

func newDefaultDeleteServer(
	cfg *config.Config, executor contracts.Executor, processManager contracts.ProcessManager,
) *defaultDeleteServer {
	return &defaultDeleteServer{
		baseCommand: newBaseCommand(cfg, executor, processManager),
		bufCommand:  bufCommand{output: components.NewSafeBuffer()},
	}
}

func (cmd *defaultDeleteServer) Execute(ctx context.Context, server *domain.Server) error {
	defer func() {
		cmd.SetComplete()
	}()

	_, err := cmd.processManager.Uninstall(ctx, server, cmd.output)
	if err != nil {
		cmd.SetResult(ErrorResult)
		_, _ = cmd.output.Write([]byte(err.Error()))
		return errors.WithMessage(err, "failed to execute process manager uninstall")
	}

	if cmd.cfg.Scripts.Delete != "" {
		_, _ = cmd.output.Write([]byte("Removing server by script...\n"))

		return cmd.removeByScript(ctx, server)
	}

	_, _ = cmd.output.Write([]byte("Removing server by filesystem...\n"))

	return cmd.removeByFilesystem(ctx, server)
}

func (cmd *defaultDeleteServer) removeByScript(ctx context.Context, server *domain.Server) error {
	command := makeFullCommand(cmd.cfg, server, cmd.cfg.Scripts.Delete, "")

	result, err := cmd.executor.ExecWithWriter(ctx, command, cmd.output, contracts.ExecutorOptions{
		WorkDir: cmd.cfg.WorkDir(),
	})
	if err != nil {
		cmd.SetComplete()
		cmd.SetResult(ErrorResult)
		_, _ = cmd.output.Write([]byte(err.Error()))
		return err
	}

	cmd.SetComplete()
	cmd.SetResult(result)

	return err
}

func (cmd *defaultDeleteServer) removeByFilesystem(_ context.Context, server *domain.Server) error {
	path := server.WorkDir(cmd.cfg)

	if cmd.isWorkDirForbiddenToRemove(path) {
		_, _ = cmd.output.Write([]byte("Forbidden work directory path, aborting removal.\n"))

		return errForbiddenWorkDirectoryPath
	}

	_, _ = cmd.output.Write([]byte("Removing directory: " + path + "\n"))

	err := os.RemoveAll(path)
	if err != nil {
		cmd.SetComplete()
		cmd.SetResult(ErrorResult)
		_, _ = cmd.output.Write([]byte(err.Error()))

		return err
	}

	cmd.SetComplete()
	cmd.SetResult(SuccessResult)

	return nil
}

func (cmd *defaultDeleteServer) isWorkDirForbiddenToRemove(path string) bool {
	path = filepath.Clean(path)
	if path == cmd.cfg.WorkPath || path == cmd.cfg.SteamCMDPath || path == "/" {
		return true
	}

	return false
}
