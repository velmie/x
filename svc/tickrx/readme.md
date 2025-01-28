# Tickrx Package

Tickrx provides a lightweight solution for scheduling and managing periodic tasks with support for concurrent execution and graceful shutdown.

## Features

- Schedule tasks to run at fixed intervals.
- Graceful shutdown with context cancellation.
- Concurrent-safe task management.

## Basic Usage

### Create a Scheduler

```go
import (
    "context"
    "github.com/velmie/x/svc/tickrx"
    "time"
)

func main() {
    scheduler := tickrx.NewScheduler()

    scheduler.Add(1*time.Second, func(ctx context.Context) {
        fmt.Println("Task executed")
    })

    // Simulate application running
    time.Sleep(10 * time.Second)

    // Stop the scheduler gracefully
    scheduler.Stop()
}
```

### Scheduling Tasks

You can schedule tasks to run at specific intervals. The task function receives a context, enabling it to check for cancellation signals and exit gracefully.

```go
scheduler := tickrx.NewScheduler()

scheduler.Add(5*time.Second, func(ctx context.Context) {
    select {
    case <-ctx.Done():
        fmt.Println("Task canceled")
        return
    default:
        fmt.Println("Task running")
    }
})

// The Stop method ensures that all running tasks are completed before exiting.
scheduler.Stop()
```