package brokerx_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/velmie/broker"
	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/brokerx"
	"github.com/velmie/x/svc/validation"
)

func ExampleHandleErrors() {
	policy := brokerx.ErrorPolicy{
		RetryDelay: time.Second,
	}
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, &apperror.Error{
			Category: apperror.Conflict,
			Code:     "CONFLICT",
		}
	}).WithMiddleware(brokerx.HandleErrors(policy), broker.RecoverPanics())
	result, _ := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	_, rejected := result.(broker.Reject)
	fmt.Println(rejected)
	// Output: true
}

func ExampleRecoverWithAttributes() {
	callback := brokerx.RecoverWithAttributes(func(_ context.Context, id string) error {
		panic("processing failed")
	}, func(id string) []slog.Attr {
		return []slog.Attr{slog.String("order_id", id)}
	})
	err := callback(context.Background(), "order-7")
	var panicErr *broker.PanicError
	fmt.Println(errors.As(err, &panicErr))
	// Output: true
}

func ExampleFieldErrors() {
	cfgErr := &broker.FieldError{
		Field:  "url",
		Reason: "required",
		Cause:  errors.New("url is required"),
	}
	err := validation.At("broker", brokerx.FieldErrors(cfgErr))
	var failure *validation.Error
	if errors.As(err, &failure) {
		fmt.Println(failure.Violations[0].Field, failure.Violations[0].Rule)
	}
	// Output: broker.url required
}

func ExamplePolicyLogConfig() {
	policy := brokerx.ErrorPolicy{
		Retryable: func(error) bool { return true },
	}
	config := brokerx.PolicyLogConfig(policy)
	fmt.Println(config.Level(broker.RetryAfter{
		Cause: errors.New("retry"),
	}, nil))
	// Output: WARN
}

func ExampleRetryErrors() {
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return nil, errors.New("storage unavailable")
	}).WithMiddleware(brokerx.RetryErrors(time.Second), broker.RecoverPanics())
	result, _ := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	retry := result.(broker.RetryAfter)
	fmt.Println(retry.Delay, retry.Cause)
	// Output: 1s storage unavailable
}

func ExampleProcessingLogConfig() {
	config := brokerx.ProcessingLogConfig()
	fmt.Println(config.Level(broker.RetryAfter{
		Cause: errors.New("retry"),
	}, nil))
	// Output: ERROR
}

func ExampleWrapProcessing() {
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return broker.Handled{}, nil
	})
	handler = brokerx.WrapProcessing(handler, slog.Default(), brokerx.ProcessingConfig{
		Errors: brokerx.RetryErrors(time.Second),
		Log:    brokerx.ProcessingLogConfig(),
	})
	result, _ := handler.Handle(context.Background(), broker.NewDelivery(broker.Message{}, "orders"))
	_, handled := result.(broker.Handled)
	fmt.Println(handled)
	// Output: true
}

func ExampleConsumerComponent() {
	consumer := testConsumer{
		validate: func(...broker.Requirement) error { return nil },
		run:      func(context.Context, broker.Handler) error { return nil },
	}
	handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
		return broker.Handled{}, nil
	})
	component, err := brokerx.ConsumerComponent("orders", consumer, handler)
	fmt.Println(component.Name, err)
	// Output: orders <nil>
}
