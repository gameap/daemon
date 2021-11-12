package game_server_commands

import (
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/interfaces"
)

const (
	UnknownResult = -1
	SuccessResult = 0
	ErrorResult   = 1
)

type ServerCommand int

const (
	invalid ServerCommand = iota
	Start
	Pause
	Unpause
	Status
	Stop
	Kill
	Restart
	Update
	Install
	Reinstall
	Delete
	end
)

type ServerCommandFactory struct {
	cfg        *config.Config
	serverRepo domain.ServerRepository
	executor interfaces.Executor
}

func NewFactory(
	cfg *config.Config,
	serverRepo domain.ServerRepository,
	executor interfaces.Executor,
) *ServerCommandFactory {
	return &ServerCommandFactory{
		cfg,
		serverRepo,
		executor,
	}
}

func (factory *ServerCommandFactory) LoadServerCommandFunc(cmd ServerCommand) interfaces.Command {
	if cmd <= invalid || cmd >= end {
		return nil
	}

	switch cmd {
	case Start:
		return newStartServer(factory.cfg, factory.executor)
	case Stop:
		return newStopServer(factory.cfg, factory.executor)
	case Restart:
		return newRestartServer(
			factory.cfg,
			factory.executor,
			newStatusServer(factory.cfg, factory.executor),
			newStopServer(factory.cfg, factory.executor),
			newStartServer(factory.cfg, factory.executor),
		)
	case Status:
		return newStatusServer(factory.cfg, factory.executor)
	case Install:
		return newInstallServer(factory.cfg, factory.executor, factory.serverRepo)
	case Update:
		return newUpdateServer(factory.cfg, factory.executor)
	case Reinstall:
		return newUpdateServer(factory.cfg, factory.executor)
	case Delete:
		return newDeleteServer(factory.cfg, factory.executor)
	}

	return nil
}

func makeFullServerPath(cfg *config.Config, serverDir string) string {
	return path.Clean(cfg.WorkPath + "/" + serverDir)
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

	command = strings.ReplaceAll(command, "{dir}", makeFullServerPath(cfg, server.Dir()))
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

	for k, v := range server.Vars() {
		command = strings.ReplaceAll(command, "{"+k+"}", v)
	}

	return command
}

type baseCommand struct {
	executor interfaces.Executor
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
