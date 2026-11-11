package examples_test

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/velmie/broker"
	"github.com/velmie/broker/azuresb"
	"github.com/velmie/broker/sqs"
	"github.com/velmie/x/svc/brokerx"
)

func TestProviderCompatibility(t *testing.T) {
	sqsConsumer, err := sqs.NewConsumer(awssqs.NewFromConfig(aws.Config{}), sqs.ConsumerConfig{
		QueueURL: "https://example.com/queue",
		Source:   "orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	azureConsumer, err := azuresb.NewConsumer(func() (azuresb.Receiver, error) {
		t.Error("unexpected receiver acquisition")
		return nil, nil
	}, azuresb.ConsumerConfig{
		Source: "orders",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		consumer broker.Consumer
	}{
		{
			name:     "sqs",
			consumer: sqsConsumer,
		},
		{
			name:     "azure",
			consumer: azureConsumer,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := broker.NewHandler(func(context.Context, broker.Delivery) (broker.Disposition, error) {
				t.Error("unexpected handler call")
				return broker.Handled{}, nil
			})
			retry := handler.WithMiddleware(brokerx.RetryErrors(0))
			if err := tc.consumer.Validate(retry.Requirements()...); err != nil {
				t.Fatalf("retry requirements: %v", err)
			}
			reject := handler.WithMiddleware(brokerx.HandleErrors(brokerx.ErrorPolicy{}))
			if err := tc.consumer.Validate(reject.Requirements()...); err == nil {
				t.Fatal("accepted rejection requirement")
			}
		})
	}
}
