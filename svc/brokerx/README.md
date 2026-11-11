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
    broker.LogProcessing(logger, brokerx.ProcessingLogConfig(policy)),
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
