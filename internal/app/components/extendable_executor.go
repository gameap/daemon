package components

import (
	"context"
	"io"
	"sync"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/pkg/shellquote"
)

type CommandHandler func(
	ctx context.Context,
	args []string,
	out io.Writer,
	options contracts.ExecutorOptions,
) (int, error)

type CommandsHandlers map[string]CommandHandler

type ExtendableExecutor struct {
	innerExecutor contracts.Executor

	mu       sync.RWMutex
	handlers CommandsHandlers
}

func NewDefaultExtendableExecutor(executor contracts.Executor) *ExtendableExecutor {
	return &ExtendableExecutor{
		handlers:      make(CommandsHandlers),
		innerExecutor: executor,
	}
}

func (executor *ExtendableExecutor) RegisterHandler(command string, handler CommandHandler) {
	executor.mu.Lock()
	defer executor.mu.Unlock()

	executor.handlers[command] = handler
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

	executor.mu.RLock()
	handler, exists := executor.handlers[handleCommand]
	executor.mu.RUnlock()

	if !exists {
		return executor.innerExecutor.ExecWithWriter(ctx, command, out, options)
	}

	return handler(ctx, args[1:], out, options)
}
