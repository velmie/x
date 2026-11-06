package request_test

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/velmie/x/svc/apperror"
	"github.com/velmie/x/svc/http/request"
	"github.com/velmie/x/svc/http/response"
)

func TestResolvePage(t *testing.T) {
	one := int64(1)
	zero := int64(0)
	negative := int64(-1)
	two := int64(2)
	hundred := int64(100)
	tooLarge := int64(101)
	beyondOffset := int64(102)
	max := int64(math.MaxInt64)
	for _, tc := range []struct {
		name       string
		page       request.Page
		policy     request.PagePolicy
		want       request.Window
		violations []apperror.Violation
	}{
		{
			name: "omitted",
			want: request.Window{
				Page:   1,
				Limit:  20,
				Offset: 0,
			},
		},
		{
			name: "configured",
			page: request.Page{
				Number: &two,
			},
			policy: request.PagePolicy{
				DefaultLimit: 5,
				MaxLimit:     10,
				MaxOffset:    5,
			},
			want: request.Window{
				Page:   2,
				Limit:  5,
				Offset: 5,
			},
		},
		{
			name: "explicit zero",
			page: request.Page{
				Number: &zero,
				Limit:  &zero,
			},
			violations: []apperror.Violation{
				{
					Field: "page",
					Rule:  apperror.RuleMin,
					Param: "1",
				},
				{
					Field: "limit",
					Rule:  apperror.RuleMin,
					Param: "1",
				},
			},
		},
		{
			name: "negative page",
			page: request.Page{
				Number: &negative,
			},
			violations: []apperror.Violation{
				{
					Field: "page",
					Rule:  apperror.RuleMin,
					Param: "1",
				},
			},
		},
		{
			name: "maximum limit",
			page: request.Page{
				Number: &one,
				Limit:  &hundred,
			},
			want: request.Window{
				Page:  1,
				Limit: 100,
			},
		},
		{
			name: "limit too large",
			page: request.Page{
				Limit: &tooLarge,
			},
			violations: []apperror.Violation{
				{
					Field: "limit",
					Rule:  apperror.RuleMax,
					Param: "100",
				},
			},
		},
		{
			name: "default maximum offset",
			page: request.Page{
				Number: &tooLarge,
				Limit:  &hundred,
			},
			want: request.Window{
				Page:   101,
				Limit:  100,
				Offset: 10000,
			},
		},
		{
			name: "beyond default maximum offset",
			page: request.Page{
				Number: &beyondOffset,
				Limit:  &hundred,
			},
			violations: []apperror.Violation{
				{
					Field: "page",
					Rule:  apperror.RuleMax,
					Param: "101",
				},
			},
		},
		{
			name: "maximum offset",
			page: request.Page{
				Number: &max,
				Limit:  &one,
			},
			policy: request.PagePolicy{
				MaxOffset: max,
			},
			want: request.Window{
				Page:   max,
				Limit:  1,
				Offset: max - 1,
			},
		},
		{
			name: "multiplication overflow",
			page: request.Page{
				Number: &max,
				Limit:  &two,
			},
			policy: request.PagePolicy{
				MaxOffset: max,
			},
			violations: []apperror.Violation{
				{
					Field: "page",
					Rule:  apperror.RuleMax,
					Param: strconv.FormatInt(max/2+1, 10),
				},
			},
		},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) {
				got, err := request.ResolvePage(tc.page, tc.policy)
				if len(tc.violations) == 0 {
					if err != nil || got != tc.want {
						t.Fatalf("ResolvePage() = %#v, %v; want %#v, nil", got, err, tc.want)
					}
					return
				}
				var app *apperror.Error
				if !errors.As(err, &app) || app.Category != apperror.Invalid || app.Code != response.ErrCodeInvalidRequestParameter {
					t.Fatalf("expected invalid request error, got %v", err)
				}
				if len(app.Violations) != len(tc.violations) {
					t.Fatalf("violations = %#v, want %#v", app.Violations, tc.violations)
				}
				for i, want := range tc.violations {
					if app.Violations[i] != want {
						t.Fatalf("violation = %#v, want %#v", app.Violations[i], want)
					}
				}
			},
		)
	}
}

func TestResolvePageInvalidPolicy(t *testing.T) {
	for _, policy := range []request.PagePolicy{
		{
			DefaultLimit: -1,
		},
		{
			MaxLimit: -1,
		},
		{
			MaxOffset: -1,
		},
		{
			DefaultLimit: 101,
		},
		{
			MaxLimit: 19,
		},
	} {
		_, err := request.ResolvePage(request.Page{}, policy)
		var app *apperror.Error
		if err == nil || errors.As(err, &app) {
			t.Fatalf("policy %#v: expected configuration error, got %v", policy, err)
		}
	}
}

func FuzzResolvePage(f *testing.F) {
	f.Add(int64(1), int64(20), int64(10000))
	f.Add(int64(math.MaxInt64), int64(2), int64(math.MaxInt64))
	f.Fuzz(
		func(t *testing.T, page int64, limit int64, maxOffset int64) {
			if maxOffset <= 0 {
				return
			}
			policy := request.PagePolicy{
				DefaultLimit: 1,
				MaxLimit:     math.MaxInt64,
				MaxOffset:    maxOffset,
			}
			got, err := request.ResolvePage(
				request.Page{
					Number: &page,
					Limit:  &limit,
				},
				policy,
			)
			valid := page >= 1 && limit >= 1 && (page-1) <= maxOffset/limit
			if valid {
				if err != nil || got.Page != page || got.Limit != limit || got.Offset != (page-1)*limit {
					t.Fatalf("valid values: window=%#v error=%v", got, err)
				}
			} else if err == nil {
				t.Fatalf("expected an error, got %#v", got)
			}
		},
	)
}
