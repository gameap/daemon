package gameservercommands

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

const (
	UnknownResult = -1
	SuccessResult = 0
	ErrorResult   = 1
)

type LoadServerCommandFunc func(cmd domain.ServerCommand) contracts.GameServerCommand

var nilLoadServerCommandFunc = func(cmd domain.ServerCommand) contracts.GameServerCommand {
	return nil
}

type ServerCommandFactory struct {
	cfg        *config.Config
	serverRepo domain.ServerRepository
	executor   contracts.Executor
}

func NewFactory(
	cfg *config.Config,
	serverRepo domain.ServerRepository,
	executor contracts.Executor,
) *ServerCommandFactory {
	return &ServerCommandFactory{
		cfg,
		serverRepo,
		executor,
	}
}

//nolint:funlen
func (factory *ServerCommandFactory) LoadServerCommand(cmd domain.ServerCommand) contracts.GameServerCommand {
	switch cmd {
	case domain.Start:
		return newStartServer(
			factory.cfg,
			factory.executor,
			factory.LoadServerCommand,
		)
	case domain.Stop, domain.Kill:
		return newStopServer(factory.cfg, factory.executor)
	case domain.Restart:
		return newRestartServer(
			factory.cfg,
			factory.executor,
			newStatusServer(factory.cfg, factory.executor),
			newStopServer(factory.cfg, factory.executor),
			newStartServer(
				factory.cfg,
				factory.executor,
				factory.LoadServerCommand,
			),
		)
	case domain.Status:
		return newStatusServer(factory.cfg, factory.executor)
	case domain.Install:
		return newInstallServer(
			factory.cfg,
			factory.executor,
			factory.serverRepo,
			newStatusServer(factory.cfg, factory.executor),
			newStopServer(factory.cfg, factory.executor),
			newStartServer(factory.cfg, factory.executor, nilLoadServerCommandFunc),
		)
	case domain.Update:
		return newUpdateServer(
			factory.cfg,
			factory.executor,
			factory.serverRepo,
			newStatusServer(factory.cfg, factory.executor),
			newStopServer(factory.cfg, factory.executor),
			newStartServer(factory.cfg, factory.executor, nilLoadServerCommandFunc),
		)
	case domain.Reinstall:
		return newCommandList(factory.cfg, factory.executor, []contracts.GameServerCommand{
			newDeleteServer(factory.cfg, factory.executor),
			newInstallServer(
				factory.cfg,
				factory.executor,
				factory.serverRepo,
				newStatusServer(factory.cfg, factory.executor),
				newStopServer(factory.cfg, factory.executor),
				newStartServer(factory.cfg, factory.executor, nilLoadServerCommandFunc),
			),
		})
	case domain.Delete:
		return newDeleteServer(factory.cfg, factory.executor)
	case domain.Pause:
	case domain.Unpause:
		return newNotImplementedCommand()
	}

	return nil
}

func makeFullCommand(
	cfg *config.Config,
	server *domain.Server,
	commandTemplate string,
	serverCommand string,
) string {
	commandTemplate = strings.Replace(commandTemplate, "{command}", serverCommand, 1)

	return replaceShortCodes(commandTemplate, cfg, server)
}

func replaceShortCodes(commandTemplate string, cfg *config.Config, server *domain.Server) string {
	command := commandTemplate

	command = strings.ReplaceAll(command, "{dir}", server.WorkDir(cfg))
	command = strings.ReplaceAll(command, "{uuid}", server.UUID())
	command = strings.ReplaceAll(command, "{uuid_short}", server.UUIDShort())
	command = strings.ReplaceAll(command, "{id}", strconv.Itoa(server.ID()))

	command = strings.ReplaceAll(command, "{host}", server.IP())
	command = strings.ReplaceAll(command, "{ip}", server.IP())
	command = strings.ReplaceAll(command, "{port}", strconv.Itoa(server.ConnectPort()))
	command = strings.ReplaceAll(command, "{query_port}", strconv.Itoa(server.QueryPort()))
	command = strings.ReplaceAll(command, "{rcon_port}", strconv.Itoa(server.RCONPort()))
	command = strings.ReplaceAll(command, "{rcon_password}", server.RCONPassword())

	command = strings.ReplaceAll(command, "{game}", server.Game().StartCode)
	command = strings.ReplaceAll(command, "{user}", server.User())

	command = strings.ReplaceAll(command, "{node_work_path}", cfg.WorkPath)
	command = strings.ReplaceAll(command, "{node_tools_path}", cfg.WorkPath+"/tools")

	for k, v := range server.Vars() {
		command = strings.ReplaceAll(command, "{"+k+"}", v)
	}

	return command
}

type baseCommand struct {
	executor contracts.Executor
	cfg      *config.Config
	complete bool
	result   int
}

func (c *baseCommand) Result() int {
	return c.result
}

func (c *baseCommand) IsComplete() bool {
	return c.complete
}

type bufCommand struct {
	output io.ReadWriter
}

func (c *bufCommand) ReadOutput() []byte {
	out, err := io.ReadAll(c.output)
	if err != nil {
		return nil
	}
	return out
}

type commandList struct {
	baseCommand

	commands []contracts.GameServerCommand
}

func newCommandList(
	cfg *config.Config,
	executor contracts.Executor,
	commands []contracts.GameServerCommand,
) *commandList {
	return &commandList{
		baseCommand: baseCommand{
			cfg:      cfg,
			executor: executor,
			complete: false,
			result:   UnknownResult,
		},
		commands: commands,
	}
}

func (c *commandList) ReadOutput() []byte {
	var output []byte
	for i := range c.commands {
		output = append(output, c.commands[i].ReadOutput()...)
	}

	return output
}

func (c *commandList) Execute(ctx context.Context, server *domain.Server) error {
	for i := range c.commands {
		err := c.commands[i].Execute(ctx, server)
		if err != nil {
			return err
		}

		if c.commands[i].Result() != SuccessResult {
			c.result = c.commands[i].Result()
			c.complete = true
			return nil
		}
	}

	c.complete = true
	c.result = 1

	return nil
}

type nilCommand struct {
	baseCommand
	bufCommand

	message    string
	resultCode int
}

func (n *nilCommand) Execute(ctx context.Context, _ *domain.Server) error {
	n.complete = true
	n.result = n.resultCode
	_, _ = n.output.Write([]byte(n.message))

	return nil
}

func newNotImplementedCommand() *nilCommand {
	return &nilCommand{
		baseCommand: baseCommand{
			complete: false,
			result:   UnknownResult,
		},
		message:    "not implemented command",
		resultCode: ErrorResult,
	}
}
