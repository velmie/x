# pathexpr

Package pathexpr compiles expressions that select values and construct objects.

```go
expr, err := pathexpr.Compile("{id: event.id}")
if err != nil { return err }
body, err := expr.Eval(map[string]any{"event": event})
```

A field suffix `?` omits missing values. `??` also omits nil and zero values;
non-nil empty maps and slices are retained. Type mismatches are errors under
both suffixes. `Expr.Inspect` exposes every syntactic path and output key,
including optional fields, for configuration policy checks.
