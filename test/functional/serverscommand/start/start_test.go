package start

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gameap/daemon/test/functional/serverscommand"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/internal/app/fsutil"
	gameservercommands "github.com/gameap/daemon/internal/app/game_server_commands"
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
		suite.Assert().Contains(
			err.Error(),
			"executable file not found: exec: \"./invalid_command.sh\": "+
				"file does not exist",
		)
	} else {
		suite.Assert().Contains(
			err.Error(),
			"executable file not found: exec: \"./invalid_command.sh\": "+
				"stat ./invalid_command.sh: no such file or directory",
		)
	}
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(1, cmd.Result())
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

func (suite *Suite) TestStartInWorkDir() {
	subDir := filepath.Join(suite.WorkPath, "server", "sub")
	suite.Require().NoError(os.MkdirAll(subDir, 0o755))
	suite.Require().NoError(fsutil.Copy("../../../servers/scripts", subDir, fsutil.CopyOptions{}))
	server := suite.GivenServerWithVars(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		map[string]string{"work_dir": "sub"},
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().NoError(err)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(gameservercommands.SuccessResult, cmd.Result())
	// command.sh appends to command_result.txt in its own working directory,
	// so the file location proves where the process ran.
	suite.Assert().FileExists(filepath.Join(subDir, serverscommand.CommandResultFile))
	suite.Assert().NoFileExists(filepath.Join(suite.WorkPath, "server", serverscommand.CommandResultFile))
}

func (suite *Suite) TestStartMissingWorkDir() {
	server := suite.GivenServerWithVars(
		serverscommand.CommandScript+" start",
		serverscommand.CommandScript+" stop",
		map[string]string{"work_dir": "missing"},
	)
	cmd := suite.CommandFactory.LoadServerCommand(domain.Start, server)

	err := cmd.Execute(context.Background(), server)

	suite.Require().Error(err)
	suite.Assert().Contains(err.Error(), "server process work directory does not exist")
	suite.Assert().Contains(err.Error(), `(work_dir "missing")`)
	suite.Assert().True(cmd.IsComplete())
	suite.Assert().Equal(gameservercommands.ErrorResult, cmd.Result())
	suite.Assert().NoFileExists(filepath.Join(suite.WorkPath, "server", serverscommand.CommandResultFile))
}
