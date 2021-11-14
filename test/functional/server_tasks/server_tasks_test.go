package servertasks

import (
	"context"
	"time"
)

func (suite *Suite) TestScheduler_ExpectTaskExecutedAndUpdated() {
	executeDate := time.Now().Add(4 * time.Second)
	suite.GivenTask(1, executeDate, 10 * time.Minute)

	suite.RunServerSchedulerWithTimeout(5 * time.Second)

	task, _ := suite.ServerTaskRepository.FindByID(context.Background(), 1)
	suite.Assert().Equal(1, task.ID)
	suite.Assert().Equal(1, task.Counter)
	suite.Assert().Equal(0, task.Repeat)
	suite.Assert().Equal(executeDate.Add(10 * time.Minute), task.ExecuteDate)
}

func (suite *Suite) TestScheduler_ExpectTaskDidNotExecuteAndUpdated() {
	executeDate := time.Now().Add(10 * time.Minute)
	suite.GivenTask(2, executeDate, 10 * time.Minute)

	suite.RunServerSchedulerWithTimeout(5 * time.Second)

	task, _ := suite.ServerTaskRepository.FindByID(context.Background(), 2)
	suite.Assert().Equal(2, task.ID)
	suite.Assert().Equal(0, task.Counter)
	suite.Assert().Equal(0, task.Repeat)
	suite.Assert().Equal(executeDate, task.ExecuteDate)
}
