package restart

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestRestartViaStartStop_ServerIsActive_ExecutedStatusStopAndStartCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	suite.Assert().Equal([]byte("status\nstop\nstart\n"), cmd.ReadOutput())
}

func (suite *Suite) TestRestartViaStartStop_ServerIsNotActive_ExecutedStatusAndStartCommands() {
	suite.GivenServerIsDown()
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	suite.Assert().Equal([]byte("status failed\nstart\n"), cmd.ReadOutput())
}

func (suite *Suite) TestRestartViaStartStop_StopFailed_ExecutedStatusAndStopCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command_fail.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Restart, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
	suite.Assert().Equal("status\nstop failed\n", string(cmd.ReadOutput()))
}
