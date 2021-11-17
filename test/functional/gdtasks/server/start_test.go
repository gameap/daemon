package server

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestStartSuccess() {
	server := suite.GivenServerWithStartCommand("./make_file_with_contents.sh")
	task := suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManager(5 * time.Second)

	suite.AssertFileContents(suite.WorkPath+"/server/file.txt", []byte("FILE CONTENTS\n"))
	suite.AssertGDTaskExist(
		domain.NewGDTask(
			1,
			task.RunAfterID(),
			server,
			task.Task(),
			"",
			domain.GDTaskStatusSuccess,
		),
	)
}

func (suite *Suite) TestStartScriptReturnFailError() {
	server := suite.GivenServerWithStartCommand("./fail.sh")
	suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManager(5 * time.Second)

	suite.Assert().FileExists(suite.WorkPath + "/server/fail_sh_executed.txt")
	suite.AssertGDTaskExist(
		domain.NewGDTask(
			1,
			0,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusError,
		),
	)
}

func (suite *Suite) TestStartNotExistenceScript() {
	server := suite.GivenServerWithStartCommand("./not_existence_script.sh")
	suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManager(5 * time.Second)

	suite.AssertGDTaskExist(
		domain.NewGDTask(
			1,
			0,
			server,
			domain.GDTaskGameServerStart,
			"",
			domain.GDTaskStatusError,
		),
	)
}

func (suite *Suite) TestStartSequenceTasks() {
	server := suite.GivenServerWithStartAndStopCommand(
		"./append_to_file.sh start",
		"./append_to_file.sh stop",
	)
	suite.GivenSequenceGDTaskForServer(server)

	suite.RunTaskManager(30 * time.Second)

	suite.AssertFileContents(suite.WorkPath+"/server/file.txt", []byte("start\nstop\nstop\nstart\nstart\n"))
}

func (suite *Suite) TestRaceTasks() {
	server := suite.GivenServerWithStartAndStopCommand(
		"./sleep_and_check.sh",
		"./sleep_and_check.sh",
	)
	suite.GivenSequenceGDTaskForServer(server)

	suite.RunTaskManager(30 * time.Second)

	suite.FileExists(suite.WorkPath + "/server/sleep_and_check.txt")
	suite.NoFileExists(suite.WorkPath + "/server/sleep_and_check_fail.txt")
}
