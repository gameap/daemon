package gameservercommands

import (
	"context"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

const (
	UnknownResult = -1
	SuccessResult = 0
	ErrorResult   = 1
)

type LoadServerCommandFunc func(cmd domain.ServerCommand, server *domain.Server) contracts.GameServerCommand

var nilLoadServerCommandFunc = func(_ domain.ServerCommand, _ *domain.Server) contracts.GameServerCommand {
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
func (factory *ServerCommandFactory) LoadServerCommand(cmd domain.ServerCommand, server *domain.Server) contracts.GameServerCommand {
	switch cmd {
	case domain.Start:
		return factory.makeStartCommand(server)
	case domain.Stop, domain.Kill:
		return factory.makeStopCommand(server)
	case domain.Restart:
		return factory.makeRestartCommand(server)
	case domain.Status:
		return factory.makeStatusCommand(server)
	case domain.Install:
		return factory.makeInstallCommand(server)
	case domain.Update:
		return factory.makeUpdateCommand(server)
	case domain.Reinstall:
		return factory.makeReinstallCommand(server)
	case domain.Delete:
		return factory.makeDeleteCommand(server)
	case domain.Pause:
	case domain.Unpause:
		return newNotImplementedCommand(factory.cfg, factory.executor)
	}

	return nil
}

func (factory *ServerCommandFactory) makeStartCommand(_ *domain.Server) contracts.GameServerCommand {
	return newStartServer(
		factory.cfg,
		factory.executor,
		factory.LoadServerCommand,
	)
}

func (factory *ServerCommandFactory) makeStopCommand(_ *domain.Server) contracts.GameServerCommand {
	return newStopServer(factory.cfg, factory.executor)
}

func (factory *ServerCommandFactory) makeRestartCommand(_ *domain.Server) contracts.GameServerCommand {
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
}

func (factory *ServerCommandFactory) makeStatusCommand(_ *domain.Server) contracts.GameServerCommand {
	return newStatusServer(factory.cfg, factory.executor)
}

func (factory *ServerCommandFactory) makeInstallCommand(_ *domain.Server) contracts.GameServerCommand {
	return newInstallServer(
		factory.cfg,
		factory.executor,
		factory.serverRepo,
		newStatusServer(factory.cfg, factory.executor),
		newStopServer(factory.cfg, factory.executor),
		newStartServer(factory.cfg, factory.executor, nilLoadServerCommandFunc),
	)
}

func (factory *ServerCommandFactory) makeUpdateCommand(_ *domain.Server) contracts.GameServerCommand {
	return newUpdateServer(
		factory.cfg,
		factory.executor,
		factory.serverRepo,
		newStatusServer(factory.cfg, factory.executor),
		newStopServer(factory.cfg, factory.executor),
		newStartServer(factory.cfg, factory.executor, nilLoadServerCommandFunc),
	)
}

func (factory *ServerCommandFactory) makeReinstallCommand(_ *domain.Server) contracts.GameServerCommand {
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
}

func (factory *ServerCommandFactory) makeDeleteCommand(_ *domain.Server) contracts.GameServerCommand {
	return newDeleteServer(factory.cfg, factory.executor)
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
	cfg      *config.Config
	executor contracts.Executor
	mutex    *sync.Mutex
	complete bool
	result   int
}

func newBaseCommand(cfg *config.Config, executor contracts.Executor) baseCommand {
	return baseCommand{
		cfg:      cfg,
		executor: executor,
		complete: false,
		result:   UnknownResult,
		mutex:    &sync.Mutex{},
	}
}

func (c *baseCommand) Result() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.result
}

func (c *baseCommand) SetResult(result int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.result = result
}

func (c *baseCommand) IsComplete() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.complete
}

func (c *baseCommand) SetComplete() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.complete = true
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
		baseCommand: newBaseCommand(cfg, executor),
		commands:    commands,
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
			c.SetResult(c.commands[i].Result())
			c.SetComplete()
			return nil
		}
	}

	c.SetComplete()
	c.SetResult(SuccessResult)

	return nil
}

type nilCommand struct {
	baseCommand
	bufCommand

	message    string
	resultCode int
}

func (n *nilCommand) Execute(_ context.Context, _ *domain.Server) error {
	n.SetComplete()
	n.SetResult(n.resultCode)

	_, _ = n.output.Write([]byte(n.message))

	return nil
}

func newNotImplementedCommand(cfg *config.Config, executor contracts.Executor) *nilCommand {
	return &nilCommand{
		baseCommand: newBaseCommand(cfg, executor),
		message:     "not implemented command",
		resultCode:  ErrorResult,
	}
}
