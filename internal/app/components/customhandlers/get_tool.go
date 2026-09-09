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
	"github.com/hashicorp/go-getter"
	"github.com/pkg/errors"
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
	fileName := filepath.Base(source)
	destination := filepath.Join(g.cfg.ToolsPath, fileName)

	// The tool is downloaded next to its destination and promoted only once it
	// is complete and executable: a tool that is already there survives a
	// failed fetch and is never seen half-written. go-getter does not replace
	// a file it finds at its destination — it resumes the download from the
	// file's size, so a tool that changed upstream came back as a splice of
	// both versions, and one no larger than the old copy was not fetched at
	// all — which is why the leftover of an interrupted run goes first.
	partial := destination + ".part"

	err := removeIfExists(partial)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to remove a partial download")
	}

	progressTracker := components.NewDownloadProgressTracker(out, 5*time.Second)

	c := getter.Client{
		Ctx:              ctx,
		Src:              args[0],
		Dst:              partial,
		Mode:             getter.ClientModeFile,
		ProgressListener: progressTracker,
	}

	_, _ = out.Write([]byte("Getting tool from " + source + " to " + destination + " ...\n"))
	err = c.Get()
	if err != nil {
		_ = os.Remove(partial)

		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to get tool")
	}

	err = os.Chmod(partial, 0700)
	if err != nil {
		_ = os.Remove(partial)
		_, _ = out.Write([]byte("Failed to chmod tool"))

		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to chmod tool")
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

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}
