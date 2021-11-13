package servertaskrepository

import (
	"context"
	"net/http"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/test/functional"
	"github.com/gameap/daemon/test/functional/repositoriestest"
)

func (suite *Suite) TestFind_Success() {
	suite.GivenAPIResponse(
		"/gdaemon_api/servers_tasks",
		http.StatusOK,
		repositoriestest.JSONApiGetServersTasks,
	)
	suite.GivenAPIResponse(
		"/gdaemon_api/servers/1",
		http.StatusOK,
		repositoriestest.JSONApiGetServerResponseBody,
	)

	tasks, err := suite.ServerTaskRepository.Find(context.Background())

	suite.Require().Nil(err)
	suite.Require().NotNil(tasks)
	suite.Assert().Len(tasks, 1)
	suite.Assert().Equal(1, tasks[0].ID)
	suite.Assert().Equal(domain.ServerTaskRestart, tasks[0].Command)
	suite.Require().NotNil(tasks[0].Server)
	suite.Assert().Equal(1, tasks[0].Server.ID())
	suite.Assert().Equal(0, tasks[0].Repeat)
	suite.Assert().Equal(10 * time.Minute, tasks[0].RepeatPeriod)
	suite.Assert().Equal(0, tasks[0].Counter)
	suite.Assert().Equal(time.Date(2021, 11, 14, 0, 0, 0, 0, time.UTC), tasks[0].ExecuteDate)
}

func (suite *Suite) TestSave_Success() {
	suite.GivenAPIResponse("/gdaemon_api/servers_tasks/2", http.StatusOK, nil)
	task := domain.NewServerTask(
		2,
		domain.ServerTaskStart,
		functional.GameServer,
		2,
		1 * time.Hour,
		10,
		time.Date(2021, 11, 14, 0, 0, 0, 0, time.UTC),
	)

	err := suite.ServerTaskRepository.Save(context.Background(), task)

	suite.Require().Nil(err)
	suite.AssertAPIPutCalled(
		"/gdaemon_api/servers_tasks/2",
		[]byte(`{"repeat":2,"repeat_period":3600,"execute_date":"2021-11-14 00:00:00"}`),
	)
}

func (suite *Suite) TestFail_Success() {
	suite.GivenAPIResponse("/gdaemon_api/servers_tasks/2/fail", http.StatusCreated, nil)
	task := domain.NewServerTask(
		2,
		domain.ServerTaskStart,
		functional.GameServer,
		2,
		1 * time.Hour,
		10,
		time.Date(2021, 11, 14, 0, 0, 0, 0, time.UTC),
	)

	err := suite.ServerTaskRepository.Fail(context.Background(), task, []byte(`output contents`))

	suite.Require().Nil(err)
	suite.AssertAPIPostCalled(
		"/gdaemon_api/servers_tasks/2/fail",
		[]byte(`{"output":"output contents"}`),
	)
}
