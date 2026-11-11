# Broker adapter examples

The provider compatibility test checks the requirements accepted by the SQS and
Azure Service Bus adapters. It does not exercise cloud delivery or dead-letter
queues.

The NATS example creates a temporary stream and durable JetStream consumer,
retries a failed admission, waits for a confirmed acknowledgement, and cancels an
active delivery before joining the consumer. Run it against a disposable NATS
server with JetStream enabled:

```sh
GOWORK=off go run ./cmd/nats -server nats://127.0.0.1:4222
```

Run `GOWORK=off go test ./...` to start a disposable NATS container for the
integration test. Docker and the `nats:2.10-alpine` image are required. The
application owns the connection and topology. It joins the consumer before
removing the stream and closing the connection.

`RetryErrors` accepts a nonnegative delay that the adapter supports. This example
uses a short fixed delay and unlimited delivery attempts. A permanent error can
therefore retry indefinitely. JetStream does not automatically provide a
dead-letter queue, and a retry after a finite delivery limit is exhausted stops
the consumer.
