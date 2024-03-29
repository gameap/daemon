package commands

import (
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
