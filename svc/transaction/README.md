# transaction

Package transaction classifies `sqltx` outcomes at a service boundary. Preserve
transaction panics and uncertain commit outcomes as failures, and treat an
error as cooperative cancellation only when every cause is an allowed leaf.

```go
err, panicked := transaction.NormalizePanic(err)
if ctxErr := ctx.Err(); ctxErr != nil && !panicked && transaction.OnlyCancellation(err, ctxErr) {
    return ctxErr
}
return err
```

For a delivery context, pass its current error to `OnlyCancellation`. An
independent deadline or cancellation remains a failure when it does not match
the delivery context's error. The function examines underlying causes, so a
wrapped error cannot match itself as an allowed value.
