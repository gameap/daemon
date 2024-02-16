package customhandlers

import (
	"context"
	"io"
	"os"
	"path/filepath"

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
	source := args[0]
	fileName := filepath.Base(source)
	destination := filepath.Join(g.cfg.ToolsPath, fileName)

	c := getter.Client{
		Ctx:  ctx,
		Src:  args[0],
		Dst:  destination,
		Mode: getter.ClientModeFile,
	}

	_, _ = out.Write([]byte("Getting tool from " + source + " to " + destination + " ..."))
	err := c.Get()
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to get tool")
	}

	err = os.Chmod(destination, 0700)
	if err != nil {
		_, _ = out.Write([]byte("Failed to chmod tool"))
		return int(domain.ErrorResult), errors.WithMessage(err, "[components.GetTool] failed to chmod tool")
	}

	return int(domain.ErrorResult), nil
}
