package autostart

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	serversloop "github.com/gameap/daemon/internal/app/servers_loop"
	"github.com/gameap/daemon/test/functional/serverscommand"
)

// commandResultFile is what the fixture scripts append to, so its content is the
// record of which commands actually ran.
const commandResultFile = "command_result.txt"

// The loop is driven through its public Run, the same way it runs in the daemon,
// so these tests wait for real time to pass rather than reaching into the tick.
// A negative case has to outlast several turns of the loop's own ticker to mean
// anything, while a positive one only waits for the start to show up.
const (
	waitForStart   = 30 * time.Second
	waitForNoStart = 16 * time.Second
)

func (suite *Suite) runLoopUntil(server *domain.Server, timeout time.Duration, condition func() bool) bool {
	suite.T().Helper()

	if err := suite.ServerRepository.Save(context.Background(), server); err != nil {
		suite.T().Fatal(err)
	}

	loop := serversloop.NewServersLoop(suite.ServerRepository, suite.CommandFactory, suite.Cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = loop.Run(ctx)
	}()

	deadline := time.Now().Add(timeout)
	satisfied := false

	for time.Now().Before(deadline) {
		if condition() {
			satisfied = true
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	<-done

	return satisfied
}

func (suite *Suite) executedCommands() string {
	suite.T().Helper()

	contents, err := os.ReadFile(filepath.Join(suite.WorkPath, "server", commandResultFile))
	if err != nil {
		return ""
	}

	return string(contents)
}

// The whole point of the loop: a server that went down on its own comes back.
func (suite *Suite) TestCrashedServerWithAutostartIsStarted() {
	suite.GivenServerIsDown()
	server := suite.GivenServerWithSettings(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		domain.Settings{"autostart": "1"},
	)

	started := suite.runLoopUntil(server, waitForStart, func() bool {
		return strings.Contains(suite.executedCommands(), "start")
	})

	suite.Assert().True(started, "the loop did not start the crashed server, executed: %q", suite.executedCommands())
	suite.Assert().False(server.IsActive(), "the failing status script keeps reporting the server as down")
}

func (suite *Suite) TestCrashedServerWithoutAutostartIsLeftDown() {
	suite.GivenServerIsDown()
	server := suite.GivenServerWithSettings(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		domain.Settings{"autostart": "0"},
	)

	started := suite.runLoopUntil(server, waitForNoStart, func() bool {
		return strings.Contains(suite.executedCommands(), "start")
	})

	suite.Assert().False(started, "the loop started a server the operator asked to stay down")
}

// A server stopped on purpose carries autostart_current=0. The loop must respect
// that even though the persistent setting says autostart is on.
func (suite *Suite) TestDeliberatelyStoppedServerIsLeftDown() {
	suite.GivenServerIsDown()
	server := suite.GivenServerWithSettings(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		domain.Settings{"autostart": "1", "autostart_current": "0"},
	)

	started := suite.runLoopUntil(server, waitForNoStart, func() bool {
		return strings.Contains(suite.executedCommands(), "start")
	})

	suite.Assert().False(started, "the loop undid a deliberate stop")
}

// A running server must be left alone: the status probe says it is up, so
// nothing else should happen to it.
func (suite *Suite) TestRunningServerIsNotStarted() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithSettings(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		domain.Settings{"autostart": "1"},
	)

	started := suite.runLoopUntil(server, waitForNoStart, func() bool {
		return strings.Contains(suite.executedCommands(), "start")
	})

	suite.Assert().False(started, "the loop started a server that was already running")
	suite.Assert().True(server.IsActive(), "the running server should be reported as active")
	suite.Assert().Contains(
		suite.executedCommands(), "status", "the loop should still have probed the server",
	)
}
