# Validation

This module provides reusable string rules, field names, and strict JSON object
and flat URL query decoding for [go-playground/validator](https://github.com/go-playground/validator).
Configure and register a validator before using it, including concurrent use:

```go
v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
v.RegisterTagNameFunc(validation.FieldNames("json"))
if err := validation.Register(v); err != nil {
    return err
}
err := validation.DecodeJSON(data, &input, v)
```

## Flat query decoding

Declare a DTO, apply service-owned defaults, and validate with the configured
native validator:

```go
type listQuery struct {
    Sort string `json:"sort" query:"single" validate:"oneof=sku -sku"`
    SKUs []string `json:"sku" validate:"max=20,unique,dive,required"`
}
input := listQuery{Sort: "sku"}
if err := validation.DecodeQuery(rawQuery, &input, v); err != nil {
    return err
}
```

`DecodeQuery` accepts a non-nil pointer to a struct. Fields use their `json`
names, or Go names when untagged. Untagged exported embedded value structs are
flattened. Unexported embedded structs, embedded pointer fields, and duplicate
Go field names across embeddings, including ignored fields, are rejected. Scalars,
pointers, scalar slices and `encoding.TextUnmarshaler` types are supported.
Gorilla v1.4.1 cannot safely bind slices of pointers to named scalar types
without `TextUnmarshaler`. Use scalar values or implement `TextUnmarshaler`
until that support exists upstream.

Names match case-insensitively. For repeated scalar names the last occurrence
wins, including across case aliases. Empty ordinary scalars leave the existing
value unchanged. Slices follow Gorilla's native binding behavior.

The only query tag is `query:"single"`. It rejects more than one occurrence,
including empty values and case aliases, with `max=1`. The decoder rejects
unknown names, nested paths and ambiguous or unsupported DTO metadata. Invalid
DTO metadata is an ordinary programming error. Validation failures are native
`validator.ValidationErrors` and can be converted with `Violations`. A decoding
failure may partially populate the destination, which must be discarded.

A `QueryError` reports the field, declared constraint, and native cause.
`Unwrap` returns the native parse or binding error unchanged. Gorilla returns a
`schema.MultiError`, which does not itself unwrap its contained errors. Complete
validator registration before concurrent use.

## Text rules

| Tag | Accepted strings |
| --- | --- |
| `nonzero_canonical_uuid` | Nonzero lowercase UUID in 8-4-4-4-12 form, with no version or variant restriction |
| `utf8` | Valid UTF-8, including empty text |
| `maxbytes=N` | At most N bytes, where N is nonnegative |
| `nonblank` | Nonempty after trimming Unicode whitespace |
| `trimmed_nonblank` | Nonempty with no surrounding Unicode whitespace |

`NonzeroCanonicalUUID` and `TrimmedNonblank` expose the corresponding predicates.
Blankness rules do not check UTF-8. Native `max` counts runes in strings; `maxbytes`
counts bytes. Apply the rules to strings or string pointers. Register replaces rules
of the same name. If registration fails, earlier registrations may have taken effect.

`FieldNames` uses explicit tag names, falls back to Go names and flattens untagged
embedded structs. Use `validate:"-"` to skip validation, including on `json:"-"`
fields. `FieldPath` removes a named root's prefix. An anonymous root with a flattened
named embed must be made a named type.

`DecodeJSON` requires one UTF-8 object, rejects unknown fields and trailing data,
then validates the destination. Empty input contains only JSON whitespace. Standard
`encoding/json` behavior applies to name matching, duplicate keys, null and escaped
surrogates. The caller owns size limits and must discard partially populated results
on error. Decoder causes and native validator errors are retained. Error ordering
is not guaranteed.

## Application violations

After identifying native validation failures, convert them for an application
error at the boundary:

```go
var failures validator.ValidationErrors
if errors.As(err, &failures) {
    return &apperror.Error{
        Category:   apperror.Invalid,
        Code:       CodeInvalidRequest,
        Message:    "request fields are invalid",
        Violations: validation.Violations(&input, failures),
        Cause:      err,
    }
}
```

`Violations` preserves failure order, configured DTO paths, `Tag()` (including
aliases) and `Param()`. It does not copy rejected values or supply messages. The
root must have the validated root struct's type, as a value or pointer. Paths can include map keys
and parameters can contain rule configuration, so ensure both are safe to expose.
An `InvalidValidationError` is a programming failure, not a field violation.

## Field diagnostics

A zero `Error` collects native validation failures with `AddStruct` and plain
Go checks with `Add`. Both retain their causes. `Merge` adds violations and
causes from another `Error`, or retains another kind of error as a cause.
`Err` returns nil when the
collector is empty. `FromStruct` converts one native result, while `Invalid`
reports one plain check. `At` copies an error and prefixes its field paths for
a containing section. Paths and rule parameters must be safe to expose.
Rejected values are not included in the generated violations.

`Error()` omits the text of direct `validator.ValidationErrors` causes and
those inside `errors.Join`, because their violations are already listed. This
changes log output, not the violations. Other cause text, including wrappers
around validator errors, is retained. All original causes remain available via
`Unwrap`, `errors.Is` and `errors.As`.

For decoded configuration, use `DottedPaths` to copy an `Error` with collection
paths in dotted notation, matching configuration field names: `endpoints[crm].url`
becomes `endpoints.crm.url`. It replaces every `[` with `.` and removes every
`]`, so keys containing `.`, `[` or `]` have ambiguous paths and are not escaped.
The cause is retained. Use it with `FromStruct`, `AddStruct`, `Invalid` and `At`.
Do not use it for HTTP payloads whose field paths are published wire contracts.
