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

For source-relative access, compile a path once and use it with the selected
source. `Set` updates interface-element maps and slices, creates missing map
branches, and leaves the input unchanged when it fails.

```go
path, err := pathexpr.CompilePath("profile.name")
if err != nil { return err }
if err := path.Set(source, "Alex"); err != nil { return err }
name, err := path.Get(source)
```
