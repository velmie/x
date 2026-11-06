# Request pagination

Resolve optional page and limit inputs into a bounded collection window:

```go
window, err := request.ResolvePage(input.Page, request.PagePolicy{})
if err != nil {
    return err
}
// Map window.Page, window.Limit and window.Offset to the service's filter.
```

Embed `request.Page` in a query DTO. Its pointers distinguish absent parameters
from explicit zero. A zero page chooses page 1 and the default limit. Zero
policy fields choose a default limit of 20, a maximum limit of 100, and a
maximum offset of 10000. Negative policy fields or a default above the maximum
are configuration errors. An invalid requested page or limit produces an
`apperror.Invalid` with `INVALID_REQUEST_PARAMETER` and field violations.

The caller chooses query decoding, filters, sorting, SQL and the response. The
resolved numbers are `int64`. Check ranges before converting to narrower types.
