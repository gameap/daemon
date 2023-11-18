package limiter

import (
	"context"
	"sync"
	"time"

	"github.com/gameap/daemon/internal/app/domain"
	log "github.com/sirupsen/logrus"
)

type CallScheduler struct {
	q               *Queue
	duration        time.Duration
	bulkCallFromNum int
	singleCallFunc  func(ctx context.Context, q *Queue) error
	bulkCallFunc    func(ctx context.Context, q *Queue) error
	logger          *log.Logger
}

func NewAPICallScheduler(
	duration time.Duration,
	bulkCallFromNum int,
	singleCallFunc func(ctx context.Context, q *Queue) error,
	bulkCallFunc func(ctx context.Context, q *Queue) error,
	logger *log.Logger,
) *CallScheduler {
	return &CallScheduler{
		q:               NewQueue(),
		duration:        duration,
		bulkCallFromNum: bulkCallFromNum,
		singleCallFunc:  singleCallFunc,
		bulkCallFunc:    bulkCallFunc,
		logger:          logger,
	}
}

func (s *CallScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.duration)

	for {
		select {
		case <-ticker.C:
			if s.q.Len() == 0 {
				continue
			}

			if s.q.Len() < s.bulkCallFromNum {
				err := s.singleCallFunc(context.TODO(), s.q)
				if err != nil {
					s.logger.Error(err)
				}
			} else {
				err := s.bulkCallFunc(context.TODO(), s.q)
				if err != nil {
					s.logger.Error(err)
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *CallScheduler) Put(server *domain.Server) {
	s.q.Put(server)
}

type Queue struct {
	q     []any
	mutex *sync.Mutex
}

func NewQueue() *Queue {
	return &Queue{
		q:     make([]any, 0),
		mutex: &sync.Mutex{},
	}
}

func (q *Queue) Put(item any) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	q.q = append(q.q, item)
}

func (q *Queue) Get() any {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.q) == 0 {
		return nil
	}

	item := q.q[0]
	q.q = q.q[1:]

	return item
}

func (q *Queue) GetN(n int) []any {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if len(q.q) == 0 {
		return nil
	}

	if len(q.q) < n {
		n = len(q.q)
	}

	items := q.q[:n]
	q.q = q.q[n:]

	return items
}

func (q *Queue) Len() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return len(q.q)
}
