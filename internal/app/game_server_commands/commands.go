package gameservercommands

import (
	"context"
	"io"
	"sync"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

const (
	UnknownResult = int(domain.UnknownResult)
	SuccessResult = int(domain.SuccessResult)
	ErrorResult   = int(domain.ErrorResult)
)

type LoadServerCommandFunc func(cmd domain.ServerCommand, server *domain.Server) contracts.GameServerCommand

var nilLoadServerCommandFunc = func(_ domain.ServerCommand, _ *domain.Server) contracts.GameServerCommand {
	return nil
}

type ServerCommandFactory struct {
	cfg            *config.Config
	serverRepo     domain.ServerRepository
	executor       contracts.Executor
	processManager contracts.ProcessManager
}

func NewFactory(
	cfg *config.Config,
	serverRepo domain.ServerRepository,
	executor contracts.Executor,
	processManager contracts.ProcessManager,
) *ServerCommandFactory {
	return &ServerCommandFactory{
		cfg,
		serverRepo,
		executor,
		processManager,
	}
}

func (factory *ServerCommandFactory) LoadServerCommand(
	cmd domain.ServerCommand,
	server *domain.Server,
) contracts.GameServerCommand {
	switch cmd {
	case domain.Start:
		return factory.makeStartCommand(server, factory.LoadServerCommand)
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
		return newNotImplementedCommand(factory.cfg, factory.executor, factory.processManager)
	}

	return nil
}

func (factory *ServerCommandFactory) makeStartCommand(
	_ *domain.Server,
	lf LoadServerCommandFunc,
) contracts.GameServerCommand {
	return newDefaultStartServer(factory.cfg, factory.executor, factory.processManager, lf)
}

func (factory *ServerCommandFactory) makeStopCommand(_ *domain.Server) contracts.GameServerCommand {
	return newDefaultStopServer(factory.cfg, factory.executor, factory.processManager)
}

func (factory *ServerCommandFactory) makeRestartCommand(server *domain.Server) contracts.GameServerCommand {
	return newDefaultRestartServer(
		factory.cfg,
		factory.executor,
		factory.processManager,
		factory.makeStatusCommand(server),
		factory.makeStopCommand(server),
		factory.makeStartCommand(server, factory.LoadServerCommand),
	)
}

func (factory *ServerCommandFactory) makeStatusCommand(_ *domain.Server) contracts.GameServerCommand {
	return newDefaultStatusServer(factory.cfg, factory.executor, factory.processManager)
}

func (factory *ServerCommandFactory) makeInstallCommand(server *domain.Server) contracts.GameServerCommand {
	return newInstallServer(
		factory.cfg,
		factory.executor,
		factory.processManager,
		factory.serverRepo,
		factory.makeStatusCommand(server),
		factory.makeStopCommand(server),
		factory.makeStartCommand(server, nilLoadServerCommandFunc),
	)
}

func (factory *ServerCommandFactory) makeUpdateCommand(server *domain.Server) contracts.GameServerCommand {
	return newUpdateServer(
		factory.cfg,
		factory.executor,
		factory.processManager,
		factory.serverRepo,
		factory.makeStatusCommand(server),
		factory.makeStopCommand(server),
		factory.makeStartCommand(server, nilLoadServerCommandFunc),
	)
}

func (factory *ServerCommandFactory) makeReinstallCommand(server *domain.Server) contracts.GameServerCommand {
	return newCommandList(factory.cfg, factory.executor, factory.processManager, []contracts.GameServerCommand{
		newDefaultDeleteServer(factory.cfg, factory.executor, factory.processManager),
		newInstallServer(
			factory.cfg,
			factory.executor,
			factory.processManager,
			factory.serverRepo,
			factory.makeStatusCommand(server),
			factory.makeStopCommand(server),
			factory.makeStartCommand(server, nilLoadServerCommandFunc),
		),
	})
}

func (factory *ServerCommandFactory) makeDeleteCommand(_ *domain.Server) contracts.GameServerCommand {
	return newDefaultDeleteServer(factory.cfg, factory.executor, factory.processManager)
}

func makeFullCommand(
	cfg *config.Config,
	server *domain.Server,
	commandTemplate string,
	serverCommand string,
) string {
	return domain.MakeFullCommand(cfg, server, commandTemplate, serverCommand)
}

type baseCommand struct {
	cfg            *config.Config
	executor       contracts.Executor
	processManager contracts.ProcessManager
	mutex          *sync.Mutex
	complete       bool
	result         int
}

func newBaseCommand(
	cfg *config.Config, executor contracts.Executor, processManager contracts.ProcessManager,
) baseCommand {
	return baseCommand{
		cfg:            cfg,
		executor:       executor,
		processManager: processManager,
		complete:       false,
		result:         UnknownResult,
		mutex:          &sync.Mutex{},
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
	processManager contracts.ProcessManager,
	commands []contracts.GameServerCommand,
) *commandList {
	return &commandList{
		baseCommand: newBaseCommand(cfg, executor, processManager),
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

func newNotImplementedCommand(
	cfg *config.Config, executor contracts.Executor, processManager contracts.ProcessManager,
) *nilCommand {
	return &nilCommand{
		baseCommand: newBaseCommand(cfg, executor, processManager),
		message:     "not implemented command",
		resultCode:  ErrorResult,
	}
}
