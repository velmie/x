package brokerx

import (
	"context"

	"github.com/velmie/broker"
	"github.com/velmie/x/bootstrap"
)

// Consumer provides local capability validation and a joined service run.
type Consumer interface {
	broker.ServiceConsumer
	Validate(requirements ...broker.Requirement) error
}

// ConsumerComponent validates handler requirements and binds the consumer to a
// lifecycle component. Validation failures are broker.RegistrationError values
// with operation "validate". Run preserves the error from RunService. All
// arguments are required; resources remain owned by their creator.
func ConsumerComponent(name string, consumer Consumer, handler broker.Handler) (bootstrap.Component, error) {
	if err := consumer.Validate(handler.Requirements()...); err != nil {
		return bootstrap.Component{}, &broker.RegistrationError{
			Name:      name,
			Operation: "validate",
			Cause:     err,
		}
	}
	return bootstrap.Component{
		Name: name,
		Run:  func(ctx context.Context) error { return consumer.RunService(ctx, handler) },
	}, nil
}
