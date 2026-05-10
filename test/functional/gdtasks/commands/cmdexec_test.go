package commands

import (
	"math/rand"
	"path/filepath"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestExecuteCommandSuccess() {
	task := suite.GivenGDTaskWithCommand(MakeFileWithContentsServerScript)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{task})

	suite.AssertFileContents(suite.WorkPath+"/file.txt", []byte("FILE CONTENTS\n"))
	suite.AssertGDTaskExist(
		domain.NewGDTask(
			task.ID(),
			task.RunAfterID(),
			nil,
			task.Task(),
			task.Command(),
			domain.GDTaskStatusSuccess,
		),
	)
}

func (suite *Suite) TestExecuteGetToolSuccess() {
	task := suite.GivenGDTaskWithCommand("get-tool https://raw.githubusercontent.com/gameap/scripts/master/fastdl/fastdl.sh")

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{task})

	suite.AssertGDTaskExist(
		domain.NewGDTask(
			task.ID(),
			task.RunAfterID(),
			nil,
			task.Task(),
			task.Command(),
			domain.GDTaskStatusSuccess,
		),
	)
	suite.Assert().FileExists(filepath.Join(suite.Cfg.ToolsPath, "/fastdl.sh"))
}

func (suite *Suite) TestExecuteSequenceTasks_PredecessorSuccess() {
	first := suite.GivenGDTaskWithCommand(MakeFileWithContentsServerScript)
	second := suite.givenDependentGDTaskWithCommand(first.ID(), MakeFileWithContentsServerScript)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{first, second})

	suite.AssertFileContents(suite.WorkPath+"/file.txt", []byte("FILE CONTENTS\nFILE CONTENTS\n"))
	suite.AssertGDTaskExist(
		domain.NewGDTask(first.ID(), 0, nil, first.Task(), first.Command(), domain.GDTaskStatusSuccess),
	)
	suite.AssertGDTaskExist(
		domain.NewGDTask(second.ID(), first.ID(), nil, second.Task(), second.Command(), domain.GDTaskStatusSuccess),
	)
}

func (suite *Suite) TestExecuteSequenceTasks_PredecessorFailed() {
	first := suite.GivenGDTaskWithCommand("./server/" + filepath.Base(FailScript))
	second := suite.givenDependentGDTaskWithCommand(first.ID(), MakeFileWithContentsServerScript)

	suite.RunTaskManagerUntilTasksCompleted([]*domain.GDTask{first, second})

	suite.AssertGDTaskExist(
		domain.NewGDTask(first.ID(), 0, nil, first.Task(), first.Command(), domain.GDTaskStatusError),
	)
	suite.AssertGDTaskExist(
		domain.NewGDTask(second.ID(), first.ID(), nil, second.Task(), second.Command(), domain.GDTaskStatusError),
	)
	suite.Assert().NoFileExists(filepath.Join(suite.WorkPath, "file.txt"))
}

func (suite *Suite) givenDependentGDTaskWithCommand(runAfterID int, cmd string) *domain.GDTask {
	suite.T().Helper()

	const minID, maxID = 100, 1000000000

	task := domain.NewGDTask(
		rand.Intn(maxID-minID)+minID,
		runAfterID,
		nil,
		domain.GDTaskCommandExecute,
		cmd,
		domain.GDTaskStatusWaiting,
	)

	suite.GDTaskRepository.Set([]*domain.GDTask{task})

	return task
}
