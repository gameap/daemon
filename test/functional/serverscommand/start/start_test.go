package start

import (
	"context"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestStartSuccess() {
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start)
	server := suite.GivenServerWithStartAndStopCommand(
		"./command.sh start",
		"./command.sh stop",
	)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(0, cmd.Result())
	suite.Assert().Equal([]byte("start\n"), cmd.ReadOutput())
}

func (suite *Suite) TestStartInvalidCommand() {
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start)
	server := suite.GivenServerWithStartAndStopCommand(
		"./invalid_command.sh",
		"./command.sh stop",
	)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NotNil(err)
	suite.Assert().Equal(
		"Executable file not found: exec: \"./invalid_command.sh\": stat ./invalid_command.sh: no such file or directory",
		err.Error(),
	)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(-1, cmd.Result())
}

func (suite *Suite) TestStartFailedCommand() {
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start)
	server := suite.GivenServerWithStartAndStopCommand(
		"./fail.sh",
		"./command.sh stop",
	)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Nil(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
	suite.Assert().Equal([]byte("command failed\n"), cmd.ReadOutput())
}
