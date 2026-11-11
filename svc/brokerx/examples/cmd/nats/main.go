// Command nats demonstrates retry and cooperative consumer shutdown with JetStream.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/velmie/broker"
	"github.com/velmie/broker/natsjs/v4"
	"github.com/velmie/x/svc/brokerx"
)

func main() {
	server := flag.String("server", nats.DefaultURL, "NATS server with JetStream enabled")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	attempts, err := runAdmission(ctx, *server)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("delivery succeeded after %d attempts; consumer stopped\n", attempts)
}

func runAdmission(ctx context.Context, server string) (attempts int, result error) {
	conn, err := nats.Connect(server, nats.Timeout(3*time.Second), nats.FlusherTimeout(time.Second), nats.NoReconnect())
	if err != nil {
		return 0, fmt.Errorf("connect to NATS: %w", err)
	}
	defer conn.Close()
	js, err := conn.JetStream(nats.MaxWait(3 * time.Second))
	if err != nil {
		return 0, fmt.Errorf("open JetStream: %w", err)
	}
	stream := "ADMISSION_" + nuid.Next()
	subject := "admission." + stream
	if _, err := js.AddStream(&nats.StreamConfig{
		Name:     stream,
		Subjects: []string{subject},
		Storage:  nats.MemoryStorage,
	}); err != nil {
		return 0, fmt.Errorf("create stream: %w", err)
	}
	defer func() {
		if err := js.DeleteStream(stream); err != nil {
			result = errors.Join(result, fmt.Errorf("delete stream: %w", err))
		}
	}()
	if _, err := js.AddConsumer(stream, &nats.ConsumerConfig{
		Durable:       "worker",
		FilterSubject: subject,
		AckPolicy:     nats.AckExplicitPolicy,
		AckWait:       5 * time.Second,
		MaxDeliver:    -1,
	}); err != nil {
		return 0, fmt.Errorf("create durable consumer: %w", err)
	}
	acknowledged := make(chan struct{}, 1)
	entered := make(chan struct{}, 1)
	consumer, err := natsjs.NewConsumer(conn, natsjs.ConsumerConfig{
		Stream:           stream,
		Consumer:         "worker",
		Subjects:         []string{subject},
		FetchTimeout:     100 * time.Millisecond,
		OperationTimeout: time.Second,
		Shutdown: broker.ShutdownPolicy{
			Mode: broker.ShutdownCancel,
		},
		Observer: natsjs.Observer{
			Record: func(_ context.Context, event natsjs.Observation) {
				if event.Operation == natsjs.OperationAck && event.Outcome == natsjs.OutcomeConfirmed {
					select {
					case acknowledged <- struct{}{}:
					default:
					}
				}
			},
		},
	})
	if err != nil {
		return 0, fmt.Errorf("create consumer: %w", err)
	}
	var count atomic.Int32
	handler := broker.NewHandler(func(ctx context.Context, delivery broker.Delivery) (broker.Disposition, error) {
		if string(delivery.Message().Body) == "shutdown" {
			entered <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if count.Add(1) == 1 {
			return nil, errors.New("temporary admission failure")
		}
		return broker.Handled{}, nil
	})
	wrapped := brokerx.WrapProcessing(handler, slog.New(slog.NewTextHandler(io.Discard, nil)), brokerx.ProcessingConfig{
		Errors: brokerx.RetryErrors(100 * time.Millisecond),
		Log:    brokerx.ProcessingLogConfig(),
	})
	component, err := brokerx.ConsumerComponent("admission", consumer, wrapped)
	if err != nil {
		return 0, fmt.Errorf("bind consumer: %w", err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- component.Run(runCtx) }()
	joined := false
	defer func() {
		stop()
		if !joined {
			result = errors.Join(result, <-done)
		}
	}()
	if _, err := js.Publish(subject, []byte("admit")); err != nil {
		return 0, fmt.Errorf("publish admission: %w", err)
	}
	select {
	case <-acknowledged:
	case err := <-done:
		joined = true
		return 0, fmt.Errorf("consumer stopped before acknowledgement: %w", err)
	case <-ctx.Done():
		return 0, fmt.Errorf("wait for acknowledgement: %w", ctx.Err())
	}
	if _, err := js.Publish(subject, []byte("shutdown")); err != nil {
		return 0, fmt.Errorf("publish shutdown exercise: %w", err)
	}
	select {
	case <-entered:
	case err := <-done:
		joined = true
		return 0, fmt.Errorf("consumer stopped before shutdown exercise: %w", err)
	case <-ctx.Done():
		return 0, fmt.Errorf("wait for active delivery: %w", ctx.Err())
	}
	stop()
	err = <-done
	joined = true
	if err != nil {
		return 0, fmt.Errorf("join consumer: %w", err)
	}
	return int(count.Load()), nil
}
