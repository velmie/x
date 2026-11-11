package brokerx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/velmie/broker"
	"github.com/velmie/x/svc/brokerx"
)

func TestConsumerComponentValidatesBeforeRun(t *testing.T) {
	cause := errors.New("unsupported capability")
	started := false
	consumer := testConsumer{
		validate: func(req ...broker.Requirement) error {
			if len(req) != 1 {
				t.Fatalf("requirements=%v", req)
			}
			if _, ok := req[0].(broker.RedeliveryRequirement); !ok {
				t.Fatalf("requirements=%v", req)
			}
			return cause
		},
		run: func(context.Context, broker.Handler) error {
			started = true
			return nil
		},
	}
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return broker.Handled{}, nil
	}).WithMiddleware(brokerx.RetryErrors(0))
	_, err := brokerx.ConsumerComponent("orders", consumer, handler)
	var registration *broker.RegistrationError
	if !errors.As(err, &registration) || registration.Name != "orders" || registration.Operation != "validate" || !errors.Is(err, cause) || started {
		t.Fatalf("error=%v started=%v", err, started)
	}
}

func TestConsumerComponentDelegatesRun(t *testing.T) {
	cause := errors.New("independent cleanup failure")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runs := 0
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return broker.Handled{}, nil
	})
	consumer := testConsumer{
		validate: func(...broker.Requirement) error { return nil },
		run: func(got context.Context, h broker.Handler) error {
			runs++
			if got != ctx {
				t.Error("context changed")
			}
			result, err := h.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
			if _, ok := result.(broker.Handled); !ok || err != nil {
				t.Errorf("handler=%v %v", result, err)
			}
			return errors.Join(ctx.Err(), cause)
		},
	}
	component, err := brokerx.ConsumerComponent("orders", consumer, handler)
	if err != nil || runs != 0 || component.Name != "orders" {
		t.Fatalf("component=%v error=%v runs=%d", component.Name, err, runs)
	}
	err = component.Run(ctx)
	if !errors.Is(err, cause) || !errors.Is(err, ctx.Err()) || runs != 1 {
		t.Fatalf("error=%v runs=%d", err, runs)
	}
}

func TestConsumerComponentCooperativeStop(t *testing.T) {
	consumer := testConsumer{
		validate: func(...broker.Requirement) error { return nil },
		run:      func(ctx context.Context, _ broker.Handler) error { <-ctx.Done(); return nil },
	}
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return broker.Handled{}, nil
	})
	component, err := brokerx.ConsumerComponent("orders", consumer, handler)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := component.Run(ctx); err != nil {
		t.Fatal(err)
	}
}

type testConsumer struct {
	validate func(...broker.Requirement) error
	run      func(context.Context, broker.Handler) error
}

func (c testConsumer) Validate(req ...broker.Requirement) error               { return c.validate(req...) }
func (c testConsumer) RunService(ctx context.Context, h broker.Handler) error { return c.run(ctx, h) }
