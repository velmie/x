package validation_test

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/schema"
	"github.com/velmie/x/svc/apperror"

	"github.com/velmie/x/svc/validation"
)

type QueryPaging struct {
	Page  *int64 `json:"page"`
	Limit *int64 `json:"limit"`
}

type Page struct {
	Number *int64 `json:"page"`
	Limit  *int64 `json:"limit"`
}

type page struct {
	Number *int64 `json:"page"`
	Limit  *int64 `json:"limit"`
}

func TestDecodeQueryEmbeddedPage(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantNumber *int64
		wantLimit  *int64
	}{
		{
			name:       "page",
			raw:        "page=2",
			wantNumber: queryInt64(2),
		},
		{
			name:      "limit",
			raw:       "limit=2",
			wantLimit: queryInt64(2),
		},
		{
			name:       "both",
			raw:        "page=2&limit=3",
			wantNumber: queryInt64(2),
			wantLimit:  queryInt64(3),
		},
		{
			name:       "case alias",
			raw:        "PAGE=2",
			wantNumber: queryInt64(2),
		},
		{
			name:       "repeated case aliases",
			raw:        "page=1&PAGE=2",
			wantNumber: queryInt64(2),
		},
		{
			name: "empty value",
			raw:  "page=",
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				var input struct {
					Page
				}
				if err := validation.DecodeQuery(test.raw, &input, newQueryValidator()); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(input.Number, test.wantNumber) || !reflect.DeepEqual(input.Limit, test.wantLimit) {
					t.Fatalf("page = %#v, want number=%v limit=%v", input.Page, test.wantNumber, test.wantLimit)
				}
			},
		)
	}
}

func TestDecodeQueryNestedEmbeddedPage(t *testing.T) {
	type PageContainer struct {
		Page
	}
	var input struct {
		PageContainer
	}
	if err := validation.DecodeQuery("page=2&limit=3", &input, newQueryValidator()); err != nil {
		t.Fatal(err)
	}
	if input.Number == nil || *input.Number != 2 || input.Limit == nil || *input.Limit != 3 {
		t.Fatalf("page = %#v", input.Page)
	}
}

func TestDecodeQueryEmbeddedPageEmptyRetainsDefault(t *testing.T) {
	initial := int64(9)
	input := struct {
		Page
	}{
		Page: Page{
			Number: &initial,
		},
	}
	if err := validation.DecodeQuery("page=", &input, newQueryValidator()); err != nil || input.Number != &initial {
		t.Fatalf("page = %#v, error = %v", input.Page, err)
	}
}

func TestDecodeQueryEmbeddedPagePreservesBindingCause(t *testing.T) {
	var input struct {
		Page
	}
	err := validation.DecodeQuery("page=invalid", &input, newQueryValidator())
	var failure *validation.QueryError
	var native schema.MultiError
	if !errors.As(err, &failure) || failure.Field != "page" || failure.Rule != apperror.RuleType ||
		failure.Param != "integer" || !errors.As(err, &native) || !strings.Contains(err.Error(), native.Error()) {
		t.Fatalf("page binding cause = %v", err)
	}
}

func queryInt64(value int64) *int64 { return &value }

type queryDTO struct {
	QueryPaging
	Count   int64     `json:"count"`
	Enabled *bool     `json:"enabled"`
	Sort    string    `json:"sort" query:"single" validate:"oneof=sku -sku"`
	SKUs    []string  `json:"sku" validate:"max=2,unique,dive,required"`
	Code    queryText `json:"code"`
}

type queryText string

func (q *queryText) UnmarshalText(data []byte) error {
	if string(data) == "bad" {
		return errors.New("text decoder fixture cause")
	}
	*q = queryText("decoded:" + string(data))
	return nil
}

func newQueryValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled(), validator.WithTagNameFuncBlankOmit())
	v.RegisterTagNameFunc(validation.FieldNames("json"))
	return v
}

func TestDecodeQueryOrderedAliasesAndDefaults(t *testing.T) {
	input := queryDTO{
		Count: 8,
		Sort:  "sku",
	}
	err := validation.DecodeQuery(
		"count=1&COUNT=2&count=3&page=&sku=a&SKU=b&enabled=true&code=good",
		&input,
		newQueryValidator(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if input.Count != 3 || input.Page != nil || input.Sort != "sku" || input.Enabled == nil || !*input.Enabled {
		t.Fatalf("input = %#v", input)
	}
	if !reflect.DeepEqual(input.SKUs, []string{"a", "b"}) || input.Code != "decoded:good" {
		t.Fatalf("input = %#v", input)
	}
	input.Count = 8
	if err := validation.DecodeQuery("count=5&COUNT=", &input, newQueryValidator()); err != nil || input.Count != 8 {
		t.Fatalf("last empty scalar changed default: %#v, %v", input, err)
	}
}

func TestDecodeQueryRejectsUnexportedEmbeddedValue(t *testing.T) {
	var input struct {
		page
	}
	assertQueryMetadataError(t, "page=2", &input, "invalid query DTO field page: embedded structs must be exported")
}

func TestDecodeQueryRejectsIgnoredUnexportedEmbeddedValue(t *testing.T) {
	var input struct {
		page `json:"-"`
	}
	assertQueryMetadataError(t, "", &input, "invalid query DTO field page: embedded structs must be exported")
}

func TestDecodeQueryRejectsNestedUnexportedEmbeddedValue(t *testing.T) {
	type PageContainer struct {
		page
	}
	var input struct {
		PageContainer
	}
	assertQueryMetadataError(t, "page=2", &input, "invalid query DTO field page: embedded structs must be exported")
}

func TestDecodeQuerySingleCountsEmptyAndAliases(t *testing.T) {
	for _, raw := range []string{"sort=&SORT=sku", "sort=sku&sort=", "SORT=&sort="} {
		input := queryDTO{
			Sort: "sku",
		}
		err := validation.DecodeQuery(raw, &input, newQueryValidator())
		var failure *validation.QueryError
		if !errors.As(err, &failure) || failure.Field != "sort" || failure.Rule != apperror.RuleMax || failure.Param != "1" {
			t.Fatalf("%q: %v", raw, err)
		}
	}
}

func TestDecodeQueryFailures(t *testing.T) {
	v := newQueryValidator()
	input := queryDTO{
		Sort:  "sku",
		Count: 9,
	}
	var failure *validation.QueryError
	for _, raw := range []string{"zeta=1&alpha=2&count=bad", "alpha=2&count=bad&zeta=1"} {
		err := validation.DecodeQuery(raw, &input, v)
		if !errors.As(err, &failure) || err.Error() != "unknown query parameter alpha" || input.Count != 9 {
			t.Fatalf("unknown field: %#v, %v", input, err)
		}
	}
	err := validation.DecodeQuery("count=%zz", &input, v)
	var escape url.EscapeError
	if !errors.As(err, &failure) || !errors.As(err, &escape) || err.Error() != "invalid URL query encoding: "+escape.Error() {
		t.Fatalf("escape: %v", err)
	}
	for _, raw := range []string{"count=bad&enabled=bad", "enabled=bad&count=bad"} {
		for i := 0; i < 30; i++ {
			err = validation.DecodeQuery(raw, &input, v)
			var native schema.MultiError
			if !errors.As(err, &failure) || failure.Field != "count" || failure.Rule != apperror.RuleType ||
				failure.Param != "integer" || !errors.As(err, &native) ||
				err.Error() != "query field count violates type=integer: "+native.Error() {
				t.Fatalf("conversion: %v", err)
			}
		}
	}
	err = validation.DecodeQuery("code=bad", &input, v)
	var native schema.MultiError
	if !errors.As(err, &native) || !strings.Contains(err.Error(), "text decoder fixture cause") {
		t.Fatalf("text decoder cause: %v", err)
	}
}

func TestDecodeQueryValidationIsNative(t *testing.T) {
	input := queryDTO{
		Sort: "sku",
	}
	err := validation.DecodeQuery("sku=a&sku=a", &input, newQueryValidator())
	var failures validator.ValidationErrors
	if !errors.As(err, &failures) || len(failures) != 1 || failures[0].Tag() != "unique" {
		t.Fatalf("validation error: %v", err)
	}
}

func TestDecodeQueryRejectsInvalidMetadata(t *testing.T) {
	for _, dst := range []any{
		&struct {
			First  string `json:"name"`
			Second string `json:"NAME"`
		}{},
		&struct {
			Value string `json:"value" query:"many"`
		}{},
		&struct {
			Nested struct {
				Value string `json:"value"`
			} `json:"nested"`
		}{},
		&struct {
			Values map[string]string `json:"values"`
		}{},
		&struct {
			Value string `json:"parent.child"`
		}{},
	} {
		err := validation.DecodeQuery("", dst, newQueryValidator())
		var failure *validation.QueryError
		if err == nil || errors.As(err, &failure) {
			t.Fatalf("%T: expected ordinary metadata error, got %v", dst, err)
		}
	}
}

func FuzzDecodeQuery(f *testing.F) {
	for _, raw := range []string{"", "page=2&limit=3", "count=1&COUNT=2", "sort=&SORT=sku", "x=%zz", "sku=a&sku=b"} {
		f.Add(raw)
	}
	f.Fuzz(
		func(t *testing.T, raw string) {
			input := queryDTO{
				Sort: "sku",
			}
			err := validation.DecodeQuery(raw, &input, newQueryValidator())
			if err != nil {
				_ = fmt.Sprint(err)
			}
		},
	)
}

func TestDecodeQueryNativeTextTypes(t *testing.T) {
	var input struct {
		Address netip.Addr `json:"address"`
		At      time.Time  `json:"at"`
	}
	err := validation.DecodeQuery("address=192.0.2.7&at=2026-01-02T03%3A04%3A05Z", &input, newQueryValidator())
	if err != nil || input.Address.String() != "192.0.2.7" || input.At.UTC().Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Fatalf("decoded = %#v, error = %v", input, err)
	}
}

type QueryEmbedded struct {
	Page *int64 `json:"page"`
}

func TestDecodeQueryRejectsEmbeddedPointer(t *testing.T) {
	type inputDTO struct {
		*QueryEmbedded
	}
	var input inputDTO
	assertQueryMetadataError(t, "PAGE=2", &input, "invalid query DTO field QueryEmbedded: embedded pointer structs are not supported")
}

func TestDecodeQueryConcurrentValidatorUse(t *testing.T) {
	v := newQueryValidator()
	for i := 0; i < 10; i++ {
		t.Run(
			fmt.Sprint(i),
			func(t *testing.T) {
				t.Parallel()
				input := queryDTO{
					Sort: "sku",
				}
				if err := validation.DecodeQuery("count=1&COUNT=2", &input, v); err != nil || input.Count != 2 {
					t.Fatalf("input = %#v, error = %v", input, err)
				}
			},
		)
	}
}

type namedQueryNumber int64

func TestDecodeQueryRejectsUnsupportedNamedPointerSlice(t *testing.T) {
	var input struct {
		Values []*namedQueryNumber `json:"value"`
	}
	err := validation.DecodeQuery("value=1", &input, newQueryValidator())
	var failure *validation.QueryError
	if err == nil || errors.As(err, &failure) {
		t.Fatalf("expected ordinary metadata error, got %v", err)
	}
}

func TestDecodeQuerySupportedPointerSlices(t *testing.T) {
	var input struct {
		Values []*int64     `json:"value"`
		Codes  []*queryText `json:"code"`
	}
	err := validation.DecodeQuery("value=1&value=2&code=ok", &input, newQueryValidator())
	if err != nil || len(input.Values) != 2 || *input.Values[0] != 1 || *input.Values[1] != 2 ||
		len(input.Codes) != 1 || *input.Codes[0] != "decoded:ok" {
		t.Fatalf("input = %#v, error = %v", input, err)
	}
}

func TestDecodeQueryRejectsUnrelatedEmbeddedPointer(t *testing.T) {
	type inputDTO struct {
		*QueryEmbedded
		Name string `json:"name"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "name=book", &input, "invalid query DTO field QueryEmbedded: embedded pointer structs are not supported")
}

type QueryNestedOne struct {
	First string `json:"first"`
}

type QueryNestedTwo struct {
	Second string `json:"second"`
}

type QueryOuter struct {
	*QueryNestedOne
	*QueryNestedTwo
}

func TestDecodeQueryRejectsNestedEmbeddedPointer(t *testing.T) {
	type inputDTO struct {
		QueryOuter
	}
	var input inputDTO
	assertQueryMetadataError(t, "first=book", &input, "invalid query DTO field QueryNestedOne: embedded pointer structs are not supported")
}

type ignoredQueryEmbed struct {
	Value string `json:"value"`
}

type QueryEmbeddedScalar string

func TestDecodeQueryRejectsIgnoredEmbeddedScalarPointer(t *testing.T) {
	type inputDTO struct {
		*QueryEmbeddedScalar `json:"-"`
		Name                 string `json:"name"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "name=book", &input, "invalid query DTO field QueryEmbeddedScalar: embedded pointer fields are not supported")
}

func TestDecodeQueryRejectsIgnoredEmbeddedPointer(t *testing.T) {
	type inputDTO struct {
		*ignoredQueryEmbed `json:"-"`
		Name               string `json:"name"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "name=book", &input, "invalid query DTO field ignoredQueryEmbed: embedded structs must be exported")
}

func TestQueryErrorIncludesAndUnwrapsCause(t *testing.T) {
	cause := errors.New("native conversion failed")
	err := &validation.QueryError{
		Field: "limit",
		Rule:  apperror.RuleType,
		Param: "integer",
		Cause: cause,
	}
	if err.Error() != "query field limit violates type=integer: native conversion failed" || !errors.Is(err, cause) {
		t.Fatalf("query error: %v", err)
	}
}

type QueryEmbedA struct {
	Value string `json:"a"`
}

type QueryEmbedB struct {
	Value string `json:"b"`
}

type QueryEmbeddedText struct {
	Value string `json:"text_value"`
}

func (q *QueryEmbeddedText) UnmarshalText(text []byte) error {
	q.Value = string(text)
	return nil
}

func TestDecodeQueryEmbeddedTextUnmarshaler(t *testing.T) {
	var input struct {
		QueryEmbeddedText `json:"text"`
	}
	if err := validation.DecodeQuery("text=book", &input, newQueryValidator()); err != nil || input.Value != "book" {
		t.Fatalf("embedded text = %#v, error = %v", input, err)
	}
}

func TestDecodeQueryRejectsCollisionWithEmbeddedTextUnmarshaler(t *testing.T) {
	type inputDTO struct {
		QueryEmbeddedText
		QueryEmbedB
	}
	var input inputDTO
	assertQueryMetadataError(t, "b=two", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func TestDecodeQueryRejectsEmbeddedFieldsWithDistinctJSONNames(t *testing.T) {
	type inputDTO struct {
		QueryEmbedA
		QueryEmbedB
	}
	var input inputDTO
	assertQueryMetadataError(t, "a=one&b=two", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func TestDecodeQueryRejectsEmbeddedNameConflictWithRootField(t *testing.T) {
	type inputDTO struct {
		QueryEmbedA
		QueryEmbedB
		Name string `json:"name"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "name=book", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func TestDecodeQueryRejectsEmbeddedFieldShadowedByNamedField(t *testing.T) {
	type inputDTO struct {
		QueryEmbedA
		Value string `json:"b"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "a=one&b=two", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func TestDecodeQueryRejectsEmbeddedFieldShadowedByIgnoredField(t *testing.T) {
	type inputDTO struct {
		QueryEmbedA
		Value string `json:"-"`
	}
	input := inputDTO{
		Value: "unchanged",
	}
	assertQueryMetadataError(t, "a=one", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func TestDecodeQueryRejectsIgnoredEmbedWithCollidingField(t *testing.T) {
	type inputDTO struct {
		QueryEmbedA
		QueryEmbedB `json:"-"`
	}
	var input inputDTO
	assertQueryMetadataError(t, "a=one", &input, "invalid query DTO field Value: Go field name is ambiguous across embedded structs")
}

func assertQueryMetadataError(t *testing.T, raw string, dst any, expected string) {
	t.Helper()
	err := validation.DecodeQuery(raw, dst, newQueryValidator())
	var queryErr *validation.QueryError
	if err == nil || err.Error() != expected || errors.As(err, &queryErr) {
		t.Fatalf("expected ordinary metadata error %q, got %v", expected, err)
	}
}
