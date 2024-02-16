package customhandlers

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/pkg/errors"
)

type outputReader interface {
	GetOutput(ctx context.Context, server *domain.Server, out io.Writer) (domain.Result, error)
}

type serverRepo interface {
	FindByID(ctx context.Context, id int) (*domain.Server, error)
}

type OutputReader struct {
	cfg        *config.Config
	getter     outputReader
	serverRepo serverRepo
}

func NewOutputReader(
	cfg *config.Config,
	getter outputReader,
	serverRepo serverRepo,
) *OutputReader {
	return &OutputReader{
		cfg:        cfg,
		getter:     getter,
		serverRepo: serverRepo,
	}
}

func (or *OutputReader) Handle(
	ctx context.Context, args []string, out io.Writer, _ contracts.ExecutorOptions,
) (int, error) {
	if len(args) < 1 {
		return int(domain.ErrorResult), errors.New("no server id provided")
	}

	serverID, err := strconv.Atoi(args[0])
	if err != nil {
		return int(domain.ErrorResult), errors.New("invalid server id, should be integer")
	}

	server, err := or.serverRepo.FindByID(ctx, serverID)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "failed to get server")
	}

	if server == nil {
		return int(domain.ErrorResult), errors.New("server not found")
	}

	result, err := or.getter.GetOutput(ctx, server, out)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "failed to get output")
	}

	return int(result), nil
}

type commandSender interface {
	SendInput(ctx context.Context, input string, server *domain.Server, out io.Writer) (domain.Result, error)
}

type CommandSender struct {
	cfg        *config.Config
	sender     commandSender
	serverRepo serverRepo
}

func NewCommandSender(
	cfg *config.Config,
	sender commandSender,
	serverRepo serverRepo,
) *CommandSender {
	return &CommandSender{
		cfg:        cfg,
		sender:     sender,
		serverRepo: serverRepo,
	}
}

func (cs *CommandSender) Handle(
	ctx context.Context, args []string, out io.Writer, _ contracts.ExecutorOptions,
) (int, error) {
	if len(args) < 2 {
		return int(domain.ErrorResult), errors.New("not enough arguments")
	}

	serverID, err := strconv.Atoi(args[0])
	if err != nil {
		return int(domain.ErrorResult), errors.New("invalid server id, should be integer")
	}

	server, err := cs.serverRepo.FindByID(ctx, serverID)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "failed to get server")
	}

	if server == nil {
		return int(domain.ErrorResult), errors.New("server not found")
	}

	b := strings.Builder{}
	b.Grow(len(args) * 10)

	for _, arg := range args[1:] {
		b.WriteString(arg)
		b.WriteString(" ")
	}

	result, err := cs.sender.SendInput(ctx, b.String(), server, out)
	if err != nil {
		return int(domain.ErrorResult), errors.WithMessage(err, "failed to send input")
	}

	return int(result), nil
}
