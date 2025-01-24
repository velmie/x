package tickrx_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/velmie/x/svc/tickrx"
)

func TestScheduler_Run(t *testing.T) {
	s := tickrx.NewScheduler()
	defer s.Stop()

	var (
		count int
		mu    sync.Mutex
		wg    sync.WaitGroup
	)

	wg.Add(1)
	s.Add(10*time.Millisecond, func(ctx context.Context) {
		mu.Lock()
		count++
		if count == 5 {
			wg.Done()
		}
		mu.Unlock()
	})

	wg.Wait() // Wait for 5 executions
	mu.Lock()
	defer mu.Unlock()
	if count != 5 {
		t.Errorf("expected 5 executions, got %d", count)
	}
}

func TestScheduler_Stop(t *testing.T) {
	s := tickrx.NewScheduler()

	var (
		count int
		mu    sync.Mutex
		wg    sync.WaitGroup
	)

	wg.Add(1)
	s.Add(10*time.Millisecond, func(ctx context.Context) {
		mu.Lock()
		count++
		if count == 1 {
			wg.Done()
		}
		mu.Unlock()
	})

	wg.Wait() // Wait for first execution
	s.Stop()

	mu.Lock()
	initial := count
	mu.Unlock()

	time.Sleep(20 * time.Millisecond) // Wait longer than interval

	mu.Lock()
	defer mu.Unlock()
	if count != initial {
		t.Errorf("count changed after stop: %d -> %d", initial, count)
	}
}

func TestGracefulStop(t *testing.T) {
	s := tickrx.NewScheduler()

	var (
		wg        sync.WaitGroup
		executing sync.WaitGroup
	)

	block := make(chan struct{})
	executing.Add(1)

	wg.Add(1)
	s.Add(10*time.Millisecond, func(ctx context.Context) {
		executing.Done()
		<-block
		wg.Done()
	})

	executing.Wait() // Ensure task started
	time.Sleep(5 * time.Millisecond)

	stopDone := make(chan struct{})
	go func() {
		s.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
		t.Fatal("stop completed before task finished")
	default:
	}

	close(block) // Release task
	<-stopDone   // Wait for stop
	wg.Wait()    // Ensure task completed
}

func TestConcurrentUsage(t *testing.T) {
	s := tickrx.NewScheduler()
	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Add(10*time.Millisecond, func(ctx context.Context) {})
		}()
	}

	// Concurrent stop
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Stop()
	}()

	wg.Wait()
}
