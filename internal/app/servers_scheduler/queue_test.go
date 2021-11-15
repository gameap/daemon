package serversscheduler

import (
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	"github.com/stretchr/testify/assert"
)

var firstTask = domain.NewServerTask(
	1,
	domain.ServerTaskStart,
	nil,
	1,
	1*time.Second,
	1,
	time.Now().Add(10*time.Minute),
)

var secondTask = domain.NewServerTask(
	2,
	domain.ServerTaskStart,
	nil,
	1,
	1*time.Second,
	5,
	time.Now().Add(20*time.Minute),
)

var thirdTask = domain.NewServerTask(
	3,
	domain.ServerTaskStart,
	nil,
	1,
	1*time.Second,
	1,
	time.Now().Add(5*time.Hour),
)

var fourthTask = domain.NewServerTask(
	4,
	domain.ServerTaskStart,
	nil,
	1,
	2*time.Second,
	1,
	time.Now().Add(11*time.Hour),
)

var fifthTask = domain.NewServerTask(
	4,
	domain.ServerTaskStart,
	nil,
	1,
	2*time.Second,
	1,
	time.Now().Add(20*time.Hour),
)

func TestPriorityQueue(t *testing.T) {
	q := newTaskQueue()
	q.Put(thirdTask)
	q.Put(firstTask)
	q.Put(fourthTask)
	q.Put(secondTask)
	q.Remove(fourthTask)
	q.Put(fifthTask)

	task1 := q.Pop()
	q.Remove(task1)
	task2 := q.Pop()
	q.Remove(task2)
	task3 := q.Pop()
	q.Remove(task3)
	task5 := q.Pop()
	q.Remove(task5)

	assert.Equal(t, firstTask, task1)
	assert.Equal(t, secondTask, task2)
	assert.Equal(t, thirdTask, task3)
	assert.Equal(t, fifthTask, task5)
}

func TestPriorityQueue_EmptyValue_ExpectNil(t *testing.T) {
	q := newTaskQueue()

	task := q.Pop()

	assert.Nil(t, task)
}
