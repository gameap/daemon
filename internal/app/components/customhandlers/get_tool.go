package customhandlers

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gameap/daemon/internal/app/components"
	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/fetch"
	"github.com/pkg/errors"
)

const (
	toolsDirPerm os.FileMode = 0o755
	toolPerm     os.FileMode = 0o700
)

type GetTool struct {
	cfg *config.Config
}

func NewGetTool(cfg *config.Config) *GetTool {
	return &GetTool{cfg: cfg}
}

func (g *GetTool) Handle(ctx context.Context, args []string, out io.Writer, _ contracts.ExecutorOptions) (int, error) {
	if len(args) < 1 {
		return int(domain.ErrorResult), errors.New("no source provided")
	}

	source := args[0]

	fileName, err := fetch.FileName(source)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to get tool")
	}

	destination := filepath.Join(g.cfg.ToolsPath, fileName)

	// The tool is downloaded next to its destination and promoted only once it
	// is complete and executable: a tool that is already there survives a
	// failed fetch and is never seen half-written.
	partial := destination + ".part"

	_, _ = out.Write([]byte("Getting tool from " + source + " to " + destination + " ...\n"))

	err = g.download(ctx, source, partial, out)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to get tool")
	}

	err = os.Rename(partial, destination)
	if err != nil {
		_ = os.Remove(partial)

		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to replace the previous tool")
	}

	err = config.UpdateEnvPath(g.cfg)
	if err != nil {
		_, _ = out.Write([]byte("Failed to update PATH with tools directories"))

		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to update PATH")
	}

	return int(domain.SuccessResult), nil
}

// download fetches source into partial and leaves it executable. Nothing is
// left behind when it fails.
func (g *GetTool) download(ctx context.Context, source, partial string, out io.Writer) error {
	err := os.MkdirAll(filepath.Dir(partial), toolsDirPerm)
	if err != nil {
		return errors.Wrap(err, "failed to create the tools directory")
	}

	// The leftover of an interrupted run goes first, and the file is created
	// anew: whatever stands under that name is neither written into nor, if it
	// is a symlink, written through.
	err = removeIfExists(partial)
	if err != nil {
		return errors.Wrap(err, "failed to remove a partial download")
	}

	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, toolPerm)
	if err != nil {
		return errors.Wrap(err, "failed to create a file to download to")
	}

	progressTracker := components.NewDownloadProgressTracker(out, 5*time.Second)

	_, err = fetch.Download(ctx, source, file, progressTracker)
	if err == nil {
		// Creating the file is subject to the umask, so the mode is set on the
		// file in hand.
		err = errors.Wrap(file.Chmod(toolPerm), "failed to chmod tool")
	}

	closeErr := file.Close()
	if err == nil {
		err = errors.Wrap(closeErr, "failed to close the downloaded tool")
	}

	if err != nil {
		_ = os.Remove(partial)
	}

	return err
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}
