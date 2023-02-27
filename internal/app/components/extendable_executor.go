package components

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gopherclass/go-shellquote"
	"github.com/hashicorp/go-getter"
	"github.com/pkg/errors"
)

type CommandHandler func(
	ctx context.Context,
	args []string,
	out io.Writer,
	options contracts.ExecutorOptions,
) (int, error)

type CommandsHandlers map[string]CommandHandler

type ExtendableExecutor struct {
	handlers      CommandsHandlers
	innerExecutor contracts.Executor
}

func NewDefaultExtendableExecutor(cfg *config.Config) *ExtendableExecutor {
	getTool := &GetTool{cfg: cfg}

	return &ExtendableExecutor{
		handlers: CommandsHandlers{
			"get-tool": getTool.Handle,
		},
		innerExecutor: NewExecutor(),
	}
}

func NewCleanDefaultExtendableExecutor(cfg *config.Config) *ExtendableExecutor {
	getTool := &GetTool{cfg: cfg}

	return &ExtendableExecutor{
		handlers: CommandsHandlers{
			"get-tool": getTool.Handle,
		},
		innerExecutor: NewCleanExecutor(),
	}
}

func (executor *ExtendableExecutor) Exec(
	ctx context.Context,
	command string,
	options contracts.ExecutorOptions,
) ([]byte, int, error) {
	buf := NewSafeBuffer()

	exitCode, err := executor.ExecWithWriter(ctx, command, buf, options)
	if err != nil {
		return nil, exitCode, err
	}

	out, err := io.ReadAll(buf)
	if err != nil {
		return nil, -1, err
	}

	return out, exitCode, err
}

func (executor *ExtendableExecutor) ExecWithWriter(
	ctx context.Context,
	command string,
	out io.Writer,
	options contracts.ExecutorOptions,
) (int, error) {
	if command == "" {
		return invalidResult, ErrEmptyCommand
	}

	args, err := shellquote.Split(command)
	if err != nil {
		return invalidResult, err
	}

	if len(args) == 0 {
		return invalidResult, ErrInvalidCommand
	}

	handleCommand := args[0]

	handler, exists := executor.handlers[handleCommand]
	if !exists {
		return executor.innerExecutor.ExecWithWriter(ctx, command, out, options)
	}

	return handler(ctx, args[1:], out, options)
}

type GetTool struct {
	cfg *config.Config
}

func (g *GetTool) Handle(ctx context.Context, args []string, out io.Writer, _ contracts.ExecutorOptions) (int, error) {
	source := args[0]
	fileName := filepath.Base(source)
	destination := path.Clean(filepath.Join(g.cfg.ToolsPath, fileName))

	c := getter.Client{
		Ctx:  ctx,
		Src:  args[0],
		Dst:  destination,
		Mode: getter.ClientModeFile,
	}

	_, _ = out.Write([]byte("Getting tool from " + source + " to " + destination + " ..."))
	err := c.Get()
	if err != nil {
		return errorResult, errors.WithMessage(err, "[components.GetTool] failed to get tool")
	}

	err = os.Chmod(destination, 0700)
	if err != nil {
		_, _ = out.Write([]byte("Failed to chmod tool"))
		return errorResult, errors.WithMessage(err, "[components.GetTool] failed to chmod tool")
	}

	return successResult, nil
}
