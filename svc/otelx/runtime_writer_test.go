package otelx

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestShutdownJoinsBlockedStdoutWriterBeforeCallerClosesIt(t *testing.T) {
	writer := &blockingWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(writer.release) }) }
	defer release()
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:         true,
			Resource:        resource.Empty(),
			NewSpanExporter: stdoutFactory(writer),
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithMaxExportBatchSize(1),
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "blocked write")
	span.End()
	<-writer.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan error, 1)
	go func() { result <- r.Shutdown(ctx) }()
	select {
	case err := <-result:
		t.Fatalf("shutdown returned before the stdout write completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if writer.closed.Load() {
		t.Fatal("runtime closed the borrowed writer")
	}
	release()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost canceled preliminary flush: %v", err)
	}
	if writer.closed.Load() || writer.writeAfterClose.Load() {
		t.Fatal("runtime closed the writer or wrote after close")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

type blockingWriter struct {
	entered         chan struct{}
	release         chan struct{}
	startOnce       sync.Once
	closed          atomic.Bool
	writeAfterClose atomic.Bool
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	w.startOnce.Do(func() { close(w.entered) })
	<-w.release
	if w.closed.Load() {
		w.writeAfterClose.Store(true)
		return 0, errors.New("writer closed")
	}
	return len(data), nil
}

func (w *blockingWriter) Close() error {
	w.closed.Store(true)
	return nil
}

func TestShutdownCapsPreliminaryFlushAndDetachesFinalCleanup(t *testing.T) {
	exporter := &deadlineSpanExporter{
		deadline: make(chan time.Time, 1),
	}
	r, err := Open(
		context.Background(),
		RuntimeConfig{
			Enabled:  true,
			Resource: resource.Empty(),
			NewSpanExporter: func(context.Context) (sdktrace.SpanExporter, error) {
				return exporter, nil
			},
			BatchOptions: []sdktrace.BatchSpanProcessorOption{
				sdktrace.WithBatchTimeout(time.Hour),
				sdktrace.WithExportTimeout(time.Second),
			},
			FlushTimeout: 100 * time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, span := r.TracerProvider.Tracer("test").Start(context.Background(), "operation")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	err = r.Shutdown(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preliminary flush deadline not reported: %v", err)
	}
	deadline := <-exporter.deadline
	if deadline.IsZero() || deadline.After(started.Add(500*time.Millisecond)) {
		t.Fatalf("flush was not bounded by configured timeout: deadline=%v", deadline)
	}
	if !exporter.shutdown.Load() || exporter.shutdownCanceled.Load() {
		t.Fatal("final exporter shutdown did not receive detached cancellation")
	}
}

type deadlineSpanExporter struct {
	deadline         chan time.Time
	shutdown         atomic.Bool
	shutdownCanceled atomic.Bool
}

func (e *deadlineSpanExporter) ExportSpans(ctx context.Context, _ []sdktrace.ReadOnlySpan) error {
	deadline, _ := ctx.Deadline()
	e.deadline <- deadline
	<-ctx.Done()
	return ctx.Err()
}

func (e *deadlineSpanExporter) Shutdown(ctx context.Context) error {
	e.shutdown.Store(true)
	e.shutdownCanceled.Store(ctx.Err() != nil)
	return nil
}
