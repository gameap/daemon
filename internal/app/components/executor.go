package components

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"

	"github.com/gopherclass/go-shellquote"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var EmptyCommandError = errors.New("empty command")

type ExecutorOptions struct {
	WorkDir string
	User    string
	Group   string
	Env     map[string]string
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
		return -1, EmptyCommandError
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

	_, _ = out.Write([]byte(fmt.Sprintf("%s# %s\n\n", options.WorkDir, command)))

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = options.WorkDir
	cmd.Stdout = out
	cmd.Stderr = out

	err = cmd.Run()
	if err != nil {
		_, ok := err.(*exec.ExitError)
		if !ok {
			log.Warning(err)

			return -1, err
		}
	}

	_, _ = out.Write([]byte("\nExited with " + strconv.Itoa(cmd.ProcessState.ExitCode()) + "\n"))

	return cmd.ProcessState.ExitCode(), nil
}
