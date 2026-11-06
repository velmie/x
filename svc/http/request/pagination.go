// Package request provides HTTP request input primitives.
// Pagination policy bounds collection windows without choosing application
// filters, SQL, handlers, or response envelopes.
package request

import (
	"errors"
	"strconv"

	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/http/response"
)

const (
	defaultPageLimit = int64(20)
	defaultMaxLimit  = int64(100)
	defaultMaxOffset = int64(10000)
	pageField        = "page"
	limitField       = "limit"
)

// Page holds optional page-number and page-size query values.
// A nil field is absent; an explicit zero is invalid.
type Page struct {
	Number *int64 `json:"page"`
	Limit  *int64 `json:"limit"`
}

// PagePolicy defines pagination defaults and maximum collection windows.
// Zero fields select a default limit of 20, a maximum limit of 100, and a
// maximum offset of 10000. Negative fields or a default above the maximum are
// invalid policy, not invalid request input.
type PagePolicy struct {
	DefaultLimit int64
	MaxLimit     int64
	MaxOffset    int64
}

// Window contains resolved pagination values.
type Window struct {
	Page   int64
	Limit  int64
	Offset int64
}

// ResolvePage applies policy and validates pagination before computing an offset.
// A zero Page uses page 1 and the policy's default limit. Invalid input returns
// an apperror.Invalid with INVALID_REQUEST_PARAMETER and min or max violations.
// Invalid policy returns an ordinary error. The offset and limits are int64;
// callers must check any narrower representation required by their storage layer.
func ResolvePage(page Page, policy PagePolicy) (Window, error) {
	if policy.DefaultLimit < 0 {
		return Window{}, errors.New("invalid pagination policy: DefaultLimit must be nonnegative")
	}
	if policy.MaxLimit < 0 {
		return Window{}, errors.New("invalid pagination policy: MaxLimit must be nonnegative")
	}
	if policy.MaxOffset < 0 {
		return Window{}, errors.New("invalid pagination policy: MaxOffset must be nonnegative")
	}
	if policy.DefaultLimit == 0 {
		policy.DefaultLimit = defaultPageLimit
	}
	if policy.MaxLimit == 0 {
		policy.MaxLimit = defaultMaxLimit
	}
	if policy.MaxOffset == 0 {
		policy.MaxOffset = defaultMaxOffset
	}
	if policy.DefaultLimit > policy.MaxLimit {
		return Window{}, errors.New("invalid pagination policy: DefaultLimit exceeds MaxLimit")
	}

	number := int64(1)
	if page.Number != nil {
		number = *page.Number
	}
	limit := policy.DefaultLimit
	if page.Limit != nil {
		limit = *page.Limit
	}
	var violations []apperror.Violation
	if number < 1 {
		violations = append(
			violations,
			apperror.Violation{
				Field: pageField,
				Rule:  apperror.RuleMin,
				Param: "1",
			},
		)
	}
	if limit < 1 {
		violations = append(
			violations,
			apperror.Violation{
				Field: limitField,
				Rule:  apperror.RuleMin,
				Param: "1",
			},
		)
	} else if limit > policy.MaxLimit {
		violations = append(
			violations,
			apperror.Violation{
				Field: limitField,
				Rule:  apperror.RuleMax,
				Param: strconv.FormatInt(policy.MaxLimit, 10),
			},
		)
	}
	if len(violations) != 0 {
		return Window{}, invalidPage(violations)
	}
	if number-1 > policy.MaxOffset/limit {
		return Window{}, invalidPage(
			[]apperror.Violation{
				{
					Field: pageField,
					Rule:  apperror.RuleMax,
					Param: strconv.FormatInt(policy.MaxOffset/limit+1, 10),
				},
			},
		)
	}
	return Window{
		Page:   number,
		Limit:  limit,
		Offset: (number - 1) * limit,
	}, nil
}

func invalidPage(violations []apperror.Violation) error {
	return &apperror.Error{
		Category:   apperror.Invalid,
		Code:       response.ErrCodeInvalidRequestParameter,
		Message:    "pagination parameters are invalid",
		Violations: violations,
	}
}
