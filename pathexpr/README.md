# pathexpr

Package pathexpr compiles expressions that select values and construct objects
from in-memory Go values. Compile configuration once and evaluate it for each
input:

```go
expr, err := pathexpr.Compile("{id: event.id, sku: event.items[0].sku}")
if err != nil {
    return err
}
body, err := expr.Eval(map[string]any{"event": event})
```

Objects created by expressions are fresh `map[string]any` values. Selected
values retain their Go types and can alias the input. Evaluation does not mutate
the input. Compiled expressions can be reused concurrently when their input is
not concurrently mutated.

## Language

```ebnf
Expr   = Object | Path | String | Number | "true" | "false" ;
Object = "{" [ Field { "," Field } ] "}" ;
Field  = Key ":" Expr [ Suffix ] | Path [ Suffix ] ;
Suffix = "?" | "??" ;
Path   = "." | Head { "." Key | "[" Index "]" } ;
Head   = Key | "[" Index "]" ;
```

- Keys are nonempty UTF-8 sequences excluding whitespace and
  `. , : { } [ ] ? ' " \ ( )`. Whitespace between tokens is ignored, but
  separated tokens are never concatenated.
- Numbers use JSON syntax and evaluate to `json.Number` with their original
  spelling. Complete literal expressions take precedence over paths. A
  `CompilePath` call always treats literal-looking components as keys.
- Single-quoted strings support `\\`, `\'`, `\n`, `\r`, and `\t`. Their
  other characters, including whitespace and punctuation, are literal.
- Indexes are unsigned decimal integers representable as `uint64`, with no
  leading zeros except `0`. Indexing does not grow a slice.
- Shorthand fields use the complete canonical path as the output key. Literal
  shorthand, repeated keys, empty components, trailing commas and suffixes
  outside fields are rejected at compilation.

Reads traverse string-keyed maps and slices, including named and typed maps
and slices. Other Go values, including structs, arrays and pointers, may be
selected as leaves but are not traversed. A missing key or an out-of-range
index wraps `ErrNotFound`. Type mismatches are reported through `TypeError` and
`EvalError`. These errors do not include input values.

A field suffix `?` omits a missing result. `??` also omits nil and zero values,
including zero structs and arrays. Non-nil empty maps and slices remain.
Neither suffix suppresses type errors. A suffix on a nested object applies to
the entire object. `Expr.Inspect` exposes every syntactic path and output key,
including optional fields, for configuration policy checks.

When decoding JSON input, use `json.Decoder.UseNumber` to preserve its number
spelling. Evaluation cannot recover precision already lost during decoding.

## Paths and assignment

For source-relative access, compile a path once and use it with the selected
source:

```go
path, err := pathexpr.CompilePath("profile.name")
if err != nil {
    return err
}
if err := path.Set(source, "Alex"); err != nil {
    return err
}
name, err := path.Get(source)
```

`Set` writes to string-keyed maps and slices whose element type is an interface,
including named types with these underlying types. It creates missing object
branches as `map[string]any`, but does not replace the root or grow slices.
A nil map cannot be written. Nil is assigned as a value, not treated as a
deletion. Failed writes leave the root unchanged. Concurrent mutation of the
input remains the caller's responsibility.
