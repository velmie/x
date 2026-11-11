# transaction

Package transaction classifies `sqltx` outcomes at a service boundary. Preserve
transaction panics and uncertain commit outcomes as failures, and treat an
error as cooperative cancellation only when every cause is an allowed leaf.

```go
err, panicked := transaction.NormalizePanic(err)
if ctx.Err() != nil && !panicked && transaction.OnlyCancellation(err, context.Canceled, context.DeadlineExceeded) {
    return ctx.Err()
}
return err
```

Pass leaf sentinels such as `context.Canceled` or `ctx.Err()` to
`OnlyCancellation`. A wrapped error cannot match itself as an allowed value:
the function examines its underlying causes.
