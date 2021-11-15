package components

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"syscall"

	"github.com/gopherclass/go-shellquote"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var ErrEmptyCommand = errors.New("empty command")

type ExecutorOptions struct {
	WorkDir string
	UID     string
	GID     string
	Env     map[string]string
}

type Executor struct {
	appendCommandAndExitCode bool
}

func NewExecutor() *Executor {
	return &Executor{appendCommandAndExitCode: true}
}

func NewClearExecutor() *Executor {
	return &Executor{appendCommandAndExitCode: false}
}

func (e *Executor) Exec(ctx context.Context, command string, options ExecutorOptions) ([]byte, int, error) {
	return Exec(ctx, command, options)
}

func (e *Executor) ExecWithWriter(
	ctx context.Context,
	command string,
	out io.Writer,
	options ExecutorOptions,
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

func Exec(ctx context.Context, command string, options ExecutorOptions) ([]byte, int, error) {
	buf := NewSafeBuffer()
	exitCode, err := ExecWithWriter(ctx, command, buf, options)
	if err != nil {
		return nil, -1, err
	}

	out, err := io.ReadAll(buf)
	if err != nil {
		return nil, -1, err
	}

	return out, exitCode, nil
}

func ExecWithWriter(ctx context.Context, command string, out io.Writer, options ExecutorOptions) (int, error) {
	if command == "" {
		return -1, ErrEmptyCommand
	}

	args, err := shellquote.Split(command)
	if err != nil {
		return -1, err
	}

	_, err = os.Stat(options.WorkDir)
	if err != nil {
		return -1, errors.Wrap(err, "invalid work directory")
	}

	_, err = os.Stat(path.Clean(options.WorkDir + "/" + args[0]))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		_, err = exec.LookPath(args[0])
		if err != nil {
			return -1, errors.Wrap(err, "executable file not found")
		}
	} else if err != nil {
		return -1, errors.Wrap(err, "executable file not found")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec
	cmd.Dir = options.WorkDir
	cmd.Stdout = out
	cmd.Stderr = out

	if options.UID != "" && options.GID != "" {
		uid, err := strconv.Atoi(options.UID)
		if err != nil {
			return -1, errors.WithMessage(err, "[game_server_commands.installator] invalid user uid")
		}

		gid, err := strconv.Atoi(options.UID)
		if err != nil {
			return -1, errors.WithMessage(err, "[game_server_commands.installator] invalid user gid")
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	err = cmd.Run()
	if err != nil {
		_, ok := err.(*exec.ExitError)
		if !ok {
			log.Warning(err)

			return -1, err
		}
	}

	return cmd.ProcessState.ExitCode(), nil
}
