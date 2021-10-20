package restart

import (
	"context"

	"github.com/gameap/daemon/internal/app/game_server_commands"
)

func (suite *Suite) TestRestartViaStartStop_ServerIsActive_ExecutedStatusStopAndStartCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommandFunc(game_server_commands.Restart)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	suite.Assert().Equal([]byte("status\nstop\nstart\n"), cmd.ReadOutput())
}

func (suite *Suite) TestRestartViaStartStop_ServerIsNotActive_ExecutedStatusAndStartCommands() {
	suite.GivenServerIsDown()
	cmd := suite.CommandFactory.LoadServerCommandFunc(game_server_commands.Restart)
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	suite.Assert().Equal([]byte("status\nstart\n"), cmd.ReadOutput())
}

func (suite *Suite) TestRestartViaStartStop_StopFailed_ExecutedStatusAndStopCommands() {
	suite.GivenServerIsActive()
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command_fail.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommandFunc(game_server_commands.Restart)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
	suite.Assert().Equal([]byte("status\nstop failed\n"), cmd.ReadOutput())
}
