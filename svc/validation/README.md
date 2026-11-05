# Validation

This module provides reusable string rules, field names and strict JSON object
decoding for [go-playground/validator](https://github.com/go-playground/validator).
Configure and register a validator before using it, including concurrent use:

```go
v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
v.RegisterTagNameFunc(validation.FieldNames("json"))
if err := validation.Register(v); err != nil {
    return err
}
err := validation.DecodeJSON(data, &input, v)
```

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
on error. Decoder causes and native validator errors are retained. Field paths and
errors may contain input values or map keys; callers must avoid logging sensitive
values. Error ordering is not guaranteed.

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
