package repositories

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func Test_fb(t *testing.T) {
	fb := NewFrequencyBarrier(1 * time.Second) // Allow one operation per second

	for i := 0; i < 10; i++ {
		go func(i int) {
			fb.Wait() // This will block until the operation can proceed
			fmt.Printf("Operation %d, %s\n", i, time.Now().Format(time.TimeOnly))
		}(i)
	}

	// Don't forget to stop the frequency barrier when you're done
	defer fb.Stop()

	// Sleep to allow all operations to complete
	time.Sleep(15 * time.Second)
}

type FrequencyBarrier struct {
	tick     *time.Ticker
	tickets  chan struct{}
	shutdown chan struct{}
	once     sync.Once
}

func NewFrequencyBarrier(freq time.Duration) *FrequencyBarrier {
	fb := &FrequencyBarrier{
		tick:     time.NewTicker(freq),
		tickets:  make(chan struct{}),
		shutdown: make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-fb.tick.C:
				select {
				case fb.tickets <- struct{}{}:
				default:
				}
			case <-fb.shutdown:
				fb.tick.Stop()
				close(fb.tickets)
				return
			}
		}
	}()

	return fb
}

func (fb *FrequencyBarrier) Wait() {
	<-fb.tickets
}

func (fb *FrequencyBarrier) Stop() {
	fb.once.Do(func() {
		close(fb.shutdown)
	})
}
