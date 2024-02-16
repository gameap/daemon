package custom_handlers

import (
	"context"
	"io"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

type outputReader interface {
	GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
}

type OutputReader struct {
	cfg    *config.Config
	getter outputReader
}

func NewOutputReader(cfg *config.Config, getter outputReader) *OutputReader {
	return &OutputReader{cfg: cfg, getter: getter}
}

func (g *OutputReader) Handle(
	_ context.Context, _ []string, _ io.Writer, _ contracts.ExecutorOptions,
) (int, error) {
	return 0, nil
}

type commandSender interface {
	SendCommand(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
}

type CommandSender struct {
	cfg    *config.Config
	sender commandSender
}

func NewCommandSender(cfg *config.Config, sender commandSender) *CommandSender {
	return &CommandSender{cfg: cfg, sender: sender}
}

func (g *CommandSender) Handle(
	_ context.Context, _ []string, _ io.Writer, _ contracts.ExecutorOptions,
) (int, error) {
	return 0, nil
}
