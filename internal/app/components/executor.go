package components

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/pkg/shellquote"
	"github.com/pkg/errors"
)

var ErrEmptyCommand = errors.New("empty command")
var ErrInvalidCommand = errors.New("invalid command")

var invalidResult = -1

type Executor struct {
	appendCommandAndExitCode bool
}

// NewExecutor returns a new executor.
// This configuration of executor will append command and exit code to the output.
func NewExecutor() *Executor {
	return &Executor{appendCommandAndExitCode: true}
}

// NewCleanExecutor returns a new executor.
// This configuration of executor will not append command and exit code to the output.
func NewCleanExecutor() *Executor {
	return &Executor{appendCommandAndExitCode: false}
}

func (e *Executor) Exec(ctx context.Context, command string, options contracts.ExecutorOptions) ([]byte, int, error) {
	return Exec(ctx, command, options)
}

func (e *Executor) ExecWithWriter(
	ctx context.Context,
	command string,
	out io.Writer,
	options contracts.ExecutorOptions,
) (int, error) {
	if e.appendCommandAndExitCode {
		_, _ = out.Write([]byte(fmt.Sprintf("%s# %s\n\n", options.WorkDir, command)))
	}

	result, err := ExecWithWriter(ctx, command, out, options)

	if e.appendCommandAndExitCode {
		_, _ = out.Write([]byte("\nExited with " + strconv.Itoa(result) + "\n"))
	}

	return result, err
}

func Exec(ctx context.Context, command string, options contracts.ExecutorOptions) ([]byte, int, error) {
	buf := NewSafeBuffer()
	exitCode, err := ExecWithWriter(ctx, command, buf, options)
	if err != nil {
		return nil, invalidResult, err
	}

	out, err := io.ReadAll(buf)
	if err != nil {
		return nil, invalidResult, err
	}

	return out, exitCode, nil
}

//nolint:funlen
func ExecWithWriter(
	ctx context.Context, command string, out io.Writer, options contracts.ExecutorOptions,
) (int, error) {
	if command == "" {
		return invalidResult, ErrEmptyCommand
	}

	args, err := shellquote.Split(command)
	if err != nil {
		return invalidResult, err
	}

	workDir := options.WorkDir
	_, err = os.Stat(workDir)
	if err != nil && options.FallbackWorkDir == "" {
		return invalidResult, errors.Wrapf(err, "invalid work directory %s", workDir)
	} else if err != nil && options.FallbackWorkDir != "" {
		_, err = os.Stat(options.FallbackWorkDir)
		if err != nil {
			return invalidResult, errors.Wrapf(err, "invalid fallback work directory %s", options.FallbackWorkDir)
		}

		workDir = options.FallbackWorkDir
	}

	name := args[0]

	if !filepath.IsAbs(name) {
		name = filepath.Join(workDir, args[0])
	}

	_, err = os.Stat(filepath.Clean(name))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		_, err = exec.LookPath(args[0])
		if err != nil {
			return invalidResult, errors.Wrap(err, "executable file not found")
		}
	} else if err != nil {
		return invalidResult, errors.Wrap(err, "executable file not found")
	}

	filteredArgs := make([]string, 0, len(args))
	for _, arg := range args[1:] {
		if arg != "" {
			filteredArgs = append(filteredArgs, strings.TrimSpace(arg))
		}
	}

	cmd := exec.CommandContext(ctx, name, filteredArgs...) //nolint:gosec
	cmd.Dir = workDir
	cmd.Stdout = out
	cmd.Stderr = out

	if options.UID != "" && options.GID != "" {
		cmd, err = setCMDSysProcCredential(cmd, options)
		if err != nil {
			return invalidResult, err
		}
	}

	var exitError *exec.ExitError
	err = cmd.Run()
	if err != nil && !errors.As(err, &exitError) {
		return cmd.ProcessState.ExitCode(), errors.Wrap(err, "failed to execute command")
	}
	if exitError != nil {
		return exitError.ExitCode(), nil
	}

	return cmd.ProcessState.ExitCode(), nil
}
