# brokerx

Package brokerx applies application error policy to broker handlers. Share one
policy between processing logs and error handling. Place `RecoverPanics` inside
`HandleErrors` so panic failures can be classified.

```go
policy := brokerx.ErrorPolicy{
    RetryDelay: time.Second,
    Retryable: isRetryable,
}
handler := broker.NewTypedHandler(decode, brokerx.RecoverWithAttributes(handle, attributes)).WithMiddleware(
    broker.LogProcessing(logger, brokerx.PolicyLogConfig(policy)),
    brokerx.HandleErrors(policy),
    broker.RecoverPanics(),
)
```

`RetryDelay` must be greater than zero. Zero requests immediate redelivery.
Expected business failures declared retryable by the policy log at WARN. Other
retry failures log at ERROR, and rejections log at INFO.

To include broker configuration violations in service validation, wrap the
validation result with its configuration prefix:

```go
err := validation.At("broker", brokerx.FieldErrors(cfg.Validate()))
```

Each broker field error becomes a violation with the same field and reason.
If its cause implements `Param() string`, that value is used as the parameter.
Other errors pass through unchanged, and converted errors retain the original
cause. See the runnable package examples for complete minimal calls.

For a consumer that must retry every processing failure, declare only the
redelivery capability:

```go
handler := broker.NewHandler(process).WithMiddleware(
    broker.LogProcessing(logger, brokerx.ProcessingLogConfig()),
    brokerx.RetryErrors(time.Second),
    broker.RecoverPanics(),
)
```

The delay must be nonnegative and supported by the adapter. Azure Service Bus
accepts zero; SQS accepts whole seconds from zero through 43,200. NATS stops
its consumer if a retry is requested after a finite `MaxDeliver` is exhausted.
Setting `MaxDeliver=-1` permits indefinite retries, including permanent errors.
JetStream does not provide an automatic dead-letter queue. Select a retry delay
and operational policy appropriate to the adapter.

`WrapProcessing` assembles optional tracing, processing logs, error policy, and
panic recovery in that order. It preserves the handler's requirements and adds
those declared by the middleware:

```go
handler = brokerx.WrapProcessing(handler, logger, brokerx.ProcessingConfig{
    Trace: processingTrace, // optional broker.HandlerMiddleware
    Errors: brokerx.RetryErrors(time.Second),
    Log: brokerx.ProcessingLogConfig(),
})
```

Tracing observes the classified outcome and supplies the context used for logs
and the callback. The supplied trace middleware must preserve results.

Bind a native consumer to the service lifecycle after constructing the handler:

```go
component, err := brokerx.ConsumerComponent("orders", consumer, handler)
if err != nil {
    return err
}
// Include component in bootstrap.Run. Close the consumer's dependencies after
// the run has returned and joined its work.
```

The binding validates handler requirements before returning. Its run function
calls the native consumer's `RunService` and preserves its error. The creator
retains ownership of the consumer and its dependencies.
