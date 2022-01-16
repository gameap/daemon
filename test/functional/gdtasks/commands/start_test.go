package commands

import (
	"runtime"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestStartSuccess() {
	server := suite.GivenServerWithStartCommand(MakeFileWithContentsScript)
	task := suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{task})

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
	server := suite.GivenServerWithStartCommand(FailScript)
	task := suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{task})

	suite.Assert().FileExists(suite.WorkPath + "/server/fail_executed.txt")
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
	task := suite.GivenGDTaskWithIDForServer(1, server)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{task})

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
		StartCommandScript,
		StopCommandScript,
	)
	tasks := suite.GivenSequenceGDTaskForServer(server)

	suite.RunTaskManagerUntilTasksCompleted(tasks)

	if runtime.GOOS == "windows" {
		suite.AssertFileContents(suite.WorkPath+"/server/file.txt", []byte("start\r\nstop\r\nstop\r\nstart\r\nstart\r\n"))
	} else {
		suite.AssertFileContents(suite.WorkPath+"/server/file.txt", []byte("start\nstop\nstop\nstart\nstart\n"))
	}
}

func (suite *Suite) TestRaceTasks() {
	server := suite.GivenServerWithStartAndStopCommand(
		SleepAndCheckScript,
		SleepAndCheckScript,
	)
	tasks := suite.GivenSequenceGDTaskForServer(server)

	suite.RunTaskManagerUntilTasksCompleted(tasks)

	suite.FileExists(suite.WorkPath + "/server/sleep_and_check.txt")
	suite.NoFileExists(suite.WorkPath + "/server/sleep_and_check_fail.txt")
}
