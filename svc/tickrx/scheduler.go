package tickrx

import (
	"context"
	"sync"
	"time"
)

// Scheduler provides functionality for scheduling and executing tasks
type Scheduler struct {
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	stopper sync.Once
}

// NewScheduler creates and returns a new Scheduler instance.
func NewScheduler() *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Add schedules a task to run at the specified interval.
//
// Example:
//
//	scheduler := tickrx.NewScheduler()
//	scheduler.Add(1*time.Second, func(ctx context.Context) {
//	    fmt.Println("Task running")
//	})
func (s *Scheduler) Add(interval time.Duration, task func(ctx context.Context)) {
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				task(s.ctx)
			case <-s.ctx.Done():
				return
			}
		}
	}()
}

// Stop stops all tasks managed by the Scheduler and waits for them to finish.
// This method is safe to call multiple times.
func (s *Scheduler) Stop() {
	s.stopper.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
}
