package limiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/gameap/daemon/internal/app/domain"
	"github.com/gameap/daemon/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func Test_Limiter(t *testing.T) {
	calledSingle := 0
	calledBulk := 0
	count := 0

	s := NewAPICallScheduler(
		10*time.Millisecond,
		5,
		func(_ context.Context, q *Queue) error {
			q.Get()
			calledSingle++
			count++
			return nil
		},
		func(_ context.Context, q *Queue) error {
			n := q.GetN(10)
			calledBulk++
			count += len(n)
			return nil
		},
		logger.NewLogger(config.Config{}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		s.Run(ctx)
		wg.Done()
	}()

	s.Put(&domain.Server{})
	s.Put(&domain.Server{})
	time.Sleep(100 * time.Millisecond)
	s.Put(&domain.Server{})
	time.Sleep(100 * time.Millisecond)
	for i := 50; i > 0; i-- {
		s.Put(&domain.Server{})
	}
	wg.Wait()

	assert.Equal(t, 3, calledSingle)
	assert.Equal(t, 5, calledBulk)
	assert.Equal(t, 53, count)
}
