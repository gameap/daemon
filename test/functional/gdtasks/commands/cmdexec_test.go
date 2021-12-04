package commands

import (
	"time"

	"github.com/gameap/daemon/internal/app/domain"
)

func (suite *Suite) TestExecuteCommandSuccess() {
	task := suite.GivenGDTaskWithCommand("./server/make_file_with_contents.sh")

	suite.RunTaskManager(2 * time.Second)

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
