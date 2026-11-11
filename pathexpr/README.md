# pathexpr

Package pathexpr compiles expressions that select values and construct objects.

```go
expr, err := pathexpr.Compile("{id: event.id}")
if err != nil { return err }
body, err := expr.Eval(map[string]any{"event": event})
```
