package components

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path"

	"github.com/gopherclass/go-shellquote"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var EmptyCommandError = errors.New("empty command")

func Exec(ctx context.Context, command string, workDir string) ([]byte, int, error) {
	buf := NewSafeBuffer()
	exitCode, err := ExecWithWriter(ctx, command, workDir, buf)
	if err != nil {
		return nil, -1, err
	}

	out, err := io.ReadAll(buf)
	if err != nil {
		return nil, -1, err
	}

	return out, exitCode, nil
}

func ExecWithWriter(ctx context.Context, command string, workDir string, out io.Writer) (int, error) {
	if command == "" {
		return -1, EmptyCommandError
	}

	args, err := shellquote.Split(command)
	if err != nil {
		return -1, err
	}

	_, err = os.Stat(workDir)
	if err != nil {
		return -1, errors.Wrap(err, "Invalid work directory")
	}

	_, err = os.Stat(path.Clean(workDir + "/" + args[0]))
	if err != nil && errors.Is(err, os.ErrNotExist) {
		_, err = exec.LookPath(args[0])
		if err != nil {
			return -1, errors.Wrap(err, "Executable file not found")
		}
	} else if err != nil {
		return -1, errors.Wrap(err, "Executable file not found")
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = workDir
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

	return cmd.ProcessState.ExitCode(), nil
}
