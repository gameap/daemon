package commandmocks

import (
	"context"

	"github.com/gameap/daemon/internal/app/contracts"
	"github.com/gameap/daemon/internal/app/domain"
)

const (
	unknownResult = -1
	successResult = 0
)

func LoadServerCommand(cmd domain.ServerCommand) contracts.GameServerCommand {
	switch cmd {
	case domain.Start:
		return newMockCommand(startServer)
	case domain.Stop:
		return newMockCommand(stopServer)
	case domain.Status:
		return newMockCommand(statusServer)
	case domain.Restart:
		return newMockCommand(startServer)
	case domain.Install, domain.Update, domain.Reinstall:
		return newMockCommand(installServer)
	case domain.Delete:
		return newMockCommand(removeServer)
	default:
		return nil
	}
}

type mockCommand struct {
	action   func(server *domain.Server)
	result   int
	complete bool
}

func newMockCommand(action func(server *domain.Server)) *mockCommand {
	return &mockCommand{
		action:   action,
		result:   unknownResult,
		complete: false,
	}
}

func (command *mockCommand) ReadOutput() []byte {
	return []byte("")
}

func (command *mockCommand) Result() int {
	return command.result
}

func (command *mockCommand) IsComplete() bool {
	return command.complete
}

func (command *mockCommand) Execute(ctx context.Context, server *domain.Server) error {
	command.result = successResult
	command.complete = true

	command.action(server)

	return nil
}

func stopServer(server *domain.Server) {
	server.SetStatus(false)
}

func statusServer(server *domain.Server) {
	server.SetStatus(true)
}

func startServer(server *domain.Server) {
	server.SetStatus(true)
}

func installServer(server *domain.Server) {
	server.SetInstallationStatus(domain.ServerInstalled)
}

func removeServer(server *domain.Server) {
	server.SetInstallationStatus(domain.ServerNotInstalled)
}
