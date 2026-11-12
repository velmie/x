# condexpr

Package condexpr compiles boolean conditions over in-memory Go values. Compile
configuration once and evaluate it for each input:

```go
expr, err := condexpr.Compile("event.type == 'order.paid' && event.totalMinor >= 100000")
if err != nil {
    return err
}
send, err := expr.Eval(map[string]any{"event": event})
```

Evaluation returns `(bool, error)` and never mutates the input. Failure returns
`false` and an error. Compiled expressions can be reused concurrently when their
input is not concurrently mutated. `Expr.Inspect().Paths` returns a caller-owned
list of every canonical path occurrence in source order, including repeated
paths, optional paths and short-circuited branches. `Info.PathPositions` is a
caller-owned slice of the same length and order, identifying the first character
of each path occurrence in the original source. Callers can use these slices to
restrict allowed roots at startup and report their positions.

## Language

```ebnf
Cond       = Or ;
Or         = And { "||" And } ;
And        = Not { "&&" Not } ;
Not        = "!" Not | Primary ;
Primary    = "(" Cond ")" | Comparison | BoolCall ;
Comparison = Operand CompareOp Operand ;
CompareOp  = "==" | "!=" | "<" | "<=" | ">" | ">=" ;
Operand    = Path [ "?" ] | String | Number | "true" | "false" | "null" | "len" "(" Operand ")" ;
BoolCall   = ( "contains" | "starts_with" | "ends_with" ) "(" Operand "," Operand ")" ;
Path       = "." | Head { "." Key | "[" Index "]" } ;
Head       = Key | "[" Index "]" ;
```

- Whitespace, including newlines, is ignored between tokens. Separated keys are
  never concatenated. `event.total>=100` and `event.total >= 100` are equivalent.
- Single-quoted strings support `\\`, `\'`, `\n`, `\r`, and `\t`. Other characters,
  including literal newlines and punctuation, are preserved. Source must be UTF-8.
- Numbers use JSON syntax. A complete number or exactly `true`, `false` or `null`
  takes precedence over a path. For example, `12.10` is a number, `12 . 10` is a
  path, and `01` is a key. Function names are keys unless followed by `(`.
- Conditions are boolean by construction. Bare paths, literals and `len(...)`
  are compile errors in boolean position. Write `event.paid == true`.
  Boolean calls cannot be comparison operands.
- `!` binds more tightly than `&&`, which binds more tightly than `||`.
  `&&` and `||` evaluate left to right and short-circuit. Other operations
  evaluate their operands left to right, including calls with a null first
  operand. An error stops evaluation.
- Nesting is limited to 128 levels, counting parentheses, `!` and function calls.
  Long flat `&&` and `||` chains do not consume nesting depth.

## Paths

Paths are compiled with `pathexpr.CompilePath` and read with `Path.Get`. Keys,
indexes and traversal follow [pathexpr](../pathexpr/README.md):

- Keys are nonempty UTF-8 sequences excluding whitespace and
  `. , : { } [ ] ? ' " \ ( )`.
- Condition paths additionally exclude `= ! < > & |`, which delimit operators.
  There is no quoted-key syntax to access keys containing these characters.
- Indexes are unsigned decimal integers representable as `uint64`, with no
  leading zeros except `0`. `.` selects the root, and `[0]` indexes a root slice.
- Reads traverse string-keyed maps and slices, including named and typed maps
  and slices. Structs, Go arrays and pointers may be selected as leaves but
  cannot be traversed.

A missing path is an evaluation error wrapping `pathexpr.ErrNotFound`. A `?`
suffix turns a missing key or out-of-range index into null. It never suppresses
a traversal type mismatch. Unlike optional fields in pathexpr object
expressions, nothing is omitted: the operand becomes null. `??` is not supported.

```text
event.items[0].sku == 'A-1'
event.discount? != null
event.discount? > 0
message.id != ''
```

## Values and operations

There is no type coercion. Values have null, boolean, number, string, array or
object kinds. Named Go boolean and string types retain their respective kinds.
Go slices and arrays are array values, and maps are object values. Only a nil
interface is null. Typed nil maps and slices retain their container kind.
Other Go types are unsupported operands.

`==` and `!=` accept different kinds and consider them unequal. Two arrays or
two objects cannot be compared with any operator. `<`, `<=`, `>` and `>=`
require two numbers or two strings. With null on either side they return false.
Other kind combinations are evaluation errors. Strings compare bytewise.

Numbers include `json.Number` and all Go integer and floating-point kinds,
including named types and `uintptr`. Integer and decimal comparisons are exact:
`9007199254740993 > 9007199254740992` and `12.10 == 12.1` are true. A finite Go
float is interpreted using its shortest round-trip decimal spelling at its
original bit size. For example, `float32(12.1)` and `float64(12.1)` both compare
equal to `12.1`. Precision already lost in a float cannot be recovered. Use
`json.Decoder.UseNumber` when decoding JSON to preserve decimal precision.

An evaluated number must be finite and use valid JSON number syntax. An
explicit exponent must be between -10000 and 10000, inclusive, even for zero.
Number literals are normalized once during compilation. Invalid literals produce
a `SyntaxError` at the literal's position. Invalid numbers selected from input
produce evaluation errors. Numeric
comparison keeps significant digits and a decimal order, without allocating
zero padding proportional to the exponent. Storage is proportional to the
number's spelling.

| Operation | Accepted input | Result |
| --- | --- | --- |
| `len(x)` | String, array or null | Unicode code points, element count, or 0 |
| `contains(x, y)` | Two strings | Whether `y` is a substring of `x` |
| `contains(x, y)` | Array and a value | Whether a scalar element equals `y` |
| `starts_with(x, y)` | Two strings | Whether `x` starts with `y` |
| `ends_with(x, y)` | Two strings | Whether `x` ends with `y` |
| Any boolean call with null as its first operand | Any second value | False |

Array membership uses scalar `==` semantics. Nested arrays and objects never
match and are skipped without error. There is no recursive container equality.
Invalid scalar numbers and unsupported scalar types still produce errors.
Other function argument kinds are evaluation errors. `len` does not accept
objects. Empty strings follow ordinary substring, prefix and suffix rules.

```text
starts_with(event.type, 'order.') && (event.currency == 'EUR' || event.currency == 'USD')
!contains(event.tags?, 'test')
len(event.items) > 1
```

## Diagnostics and configuration

`SyntaxError` carries a byte offset and one-based line and Unicode column.
CRLF counts as one line break. `Position` and `SyntaxError` reuse pathexpr's
types. `EvalError` carries the operation, its paths, a source position and an
unwrappable cause. Operation errors point to the operator or function. Path
errors point to the failing component and preserve `pathexpr.EvalError`,
`pathexpr.TypeError` and `pathexpr.ErrNotFound`, with positions adjusted to the
whole condition. Path read failures use pathexpr's own wording. Other failures
name the operation, position and cause once, followed by distinct operand paths
when present. Errors never include input values.

In YAML, a condition starting with `!` must be quoted or written as a block
scalar because YAML otherwise interprets it as a tag:

```yaml
when: >-
  !contains(event.tags?, 'test')
```

The language has no arithmetic, regular expressions, `in`, container literals,
custom functions, date/time operations, conversions or truthiness. It does not
provide compatibility with other expression languages. The module depends only
on pathexpr and the Go standard library.
