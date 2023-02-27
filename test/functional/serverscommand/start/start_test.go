package start

import (
	"context"
	"runtime"

	"github.com/gameap/daemon/test/functional/serverscommand"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestStartSuccess() {
	server := suite.GivenServerWithStartAndStopCommand(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	//nolint:goconst
	if runtime.GOOS == "windows" {
		suite.Assert().Equal([]byte("start\r\n"), cmd.ReadOutput())
	} else {
		suite.Assert().Equal([]byte("start\n"), cmd.ReadOutput())
	}
}

func (suite *Suite) TestStartInvalidCommand() {
	server := suite.GivenServerWithStartAndStopCommand(
		"./invalid_command.sh",
		"./command.sh stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NotNil(err)
	if runtime.GOOS == "windows" {
		suite.Assert().Equal(
			"[game_server_commands.startServer] failed to execute start command: "+
				"executable file not found: exec: \"./invalid_command.sh\": "+
				"file does not exist",
			err.Error(),
		)
	} else {
		suite.Assert().Equal(
			"[game_server_commands.startServer] failed to execute start command: "+
				"executable file not found: exec: \"./invalid_command.sh\": "+
				"stat ./invalid_command.sh: no such file or directory",
			err.Error(),
		)
	}
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(-1, cmd.Result())
}

func (suite *Suite) TestStartFailedCommand() {
	server := suite.GivenServerWithStartAndStopCommand(
		serverscommand.FailScript,
		serverscommand.CommandScript+" stop",
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
	if runtime.GOOS == "windows" {
		suite.Assert().Equal([]byte("command failed\r\n"), cmd.ReadOutput())
	} else {
		suite.Assert().Equal([]byte("command failed\n"), cmd.ReadOutput())
	}
}
