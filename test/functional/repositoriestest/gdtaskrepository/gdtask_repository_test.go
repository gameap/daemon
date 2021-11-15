package gdtaskrepository

import (
	"context"
	"net/http"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/functional/repositoriestest"
)

func (suite *Suite) TestFindByStatus_Success() {
	suite.GivenAPIResponse(
		"/gdaemon_api/tasks?append=status_num&filter%5Bstatus%5D=waiting",
		http.StatusOK,
		jsonWaitingTasksResponseBody,
	)
	suite.GivenAPIResponse("/gdaemon_api/servers/1", http.StatusOK, repositoriestest.JSONApiGetServerResponseBody)

	gdtasks, err := suite.GDTaskRepository.FindByStatus(context.Background(), domain.GDTaskStatusWaiting)

	suite.Require().Nil(err)
	suite.Require().NotNil(gdtasks)
	suite.Require().Len(gdtasks, 1)
	suite.Equal(3, gdtasks[0].ID())
	suite.Equal(domain.GDTaskGameServerInstall, gdtasks[0].Task())
	suite.Equal(domain.GDTaskStatusWaiting, gdtasks[0].Status())
	suite.Require().NotNil(gdtasks[0].Server())
	suite.Equal(1, gdtasks[0].Server().ID())
}

func (suite *Suite) TestSave_Success() {
	suite.GivenAPIResponse("/gdaemon_api/tasks/2", http.StatusOK, nil)
	gdTask := domain.NewGDTask(
		2,
		0,
		functional.GameServer,
		domain.GDTaskGameServerStart,
		"",
		domain.GDTaskStatusSuccess,
	)

	err := suite.GDTaskRepository.Save(context.Background(), gdTask)

	suite.Require().Nil(err)
	suite.AssertAPIPutCalled(
		"/gdaemon_api/tasks/2",
		[]byte(`{"status":4}`),
	)
}

func (suite *Suite) TestAppendOutput_Success() {
	suite.GivenAPIResponse("/gdaemon_api/tasks/2/output", http.StatusOK, nil)
	gdTask := domain.NewGDTask(
		2,
		0,
		functional.GameServer,
		domain.GDTaskGameServerStart,
		"",
		domain.GDTaskStatusSuccess,
	)

	err := suite.GDTaskRepository.AppendOutput(context.Background(), gdTask, []byte("output contents"))

	suite.Require().Nil(err)
	suite.AssertAPIPutCalled(
		"/gdaemon_api/tasks/2/output",
		[]byte(`{"output":"output contents"}`),
	)
}
