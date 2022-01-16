package restart

import (
	"context"
	"runtime"

	"github.com/gameap/daemon/pkg/sys"
	"github.com/gameap/daemon/test/functional/serverscommand"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestRestartViaStartStop_ServerIsActive_ExecutedStatusStopAndStartCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	if runtime.GOOS == sys.Windows {
		suite.Assert().Equal([]byte("status\r\nstop\r\nstart\r\n"), cmd.ReadOutput())
	} else {
		suite.Assert().Equal([]byte("status\nstop\nstart\n"), cmd.ReadOutput())
	}
}

func (suite *Suite) TestRestartViaStartStop_ServerIsNotActive_ExecutedStatusAndStartCommands() {
	suite.GivenServerIsDown()
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart)
	server := suite.GivenServerWithStartAndStopCommand(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
	)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	if runtime.GOOS == "windows" {
		suite.Assert().Equal([]byte("status failed\r\nstart\r\n"), cmd.ReadOutput())
	} else {
		suite.Assert().Equal([]byte("status failed\nstart\n"), cmd.ReadOutput())
	}
}

func (suite *Suite) TestRestartViaStartStop_StopFailed_ExecutedStatusAndStopCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		serverscommand.CommandScript+" start",
		serverscommand.CommandFailScript+" stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
	if runtime.GOOS == "windows" {
		suite.Assert().Equal("status\r\nstop failed\r\n", string(cmd.ReadOutput()))
	} else {
		suite.Assert().Equal("status\nstop failed\n", string(cmd.ReadOutput()))
	}
}
