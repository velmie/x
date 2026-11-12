package validation_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"

	"github.com/velmie/x/svc/validation"
)

func TestDottedPathsCollections(t *testing.T) {
	type endpoint struct {
		URL string `k:"url" validate:"required"`
	}
	type config struct {
		Endpoints map[string]endpoint   `k:"endpoints" validate:"dive,keys,lowercase,endkeys"`
		Routes    []endpoint            `k:"routes" validate:"dive"`
		Nested    map[string][]endpoint `k:"nested" validate:"dive,dive"`
	}
	cfg := config{
		Endpoints: map[string]endpoint{
			"BAD_name": {},
		},
		Routes: []endpoint{
			{},
		},
		Nested: map[string][]endpoint{
			"x": {
				{},
			},
		},
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	cause := v.Struct(cfg)
	original := validation.FromStruct(cfg, cause)
	var before *validation.Error
	if !errors.As(original, &before) {
		t.Fatal("missing original validation error")
	}
	snapshot := append([]apperror.Violation(nil), before.Violations...)

	var collected validation.Error
	collected.AddStruct(cfg, cause)
	for _, input := range []error{original, collected.Err()} {
		for _, got := range []error{
			validation.DottedPaths(validation.At("config", input)),
			validation.At("config", validation.DottedPaths(input)),
		} {
			var failure *validation.Error
			var native validator.ValidationErrors
			if !errors.As(got, &failure) || !errors.As(got, &native) || !reflect.DeepEqual(native, cause) {
				t.Fatalf("lost error or cause: %v", got)
			}
			want := []apperror.Violation{
				{
					Field: "config.endpoints.BAD_name",
					Rule:  "lowercase",
				},
				{
					Field: "config.endpoints.BAD_name.url",
					Rule:  "required",
				},
				{
					Field: "config.routes.0.url",
					Rule:  "required",
				},
				{
					Field: "config.nested.x.0.url",
					Rule:  "required",
				},
			}
			if !reflect.DeepEqual(failure.Violations, want) {
				t.Fatalf("violations = %#v, want %#v", failure.Violations, want)
			}
		}
	}
	if !reflect.DeepEqual(before.Violations, snapshot) {
		t.Fatalf("original changed: %#v", before.Violations)
	}
}

func TestDottedPathsOtherInputs(t *testing.T) {
	if validation.DottedPaths(nil) != nil {
		t.Fatal("nil changed")
	}
	foreign := errors.New("foreign")
	wrapped := fmt.Errorf("wrapped: %w", validation.Invalid("a[x]", "required", "", foreign))
	for _, err := range []error{foreign, wrapped} {
		if validation.DottedPaths(err) != err {
			t.Fatal("foreign error changed")
		}
	}
	for _, tc := range []struct {
		field string
		want  string
	}{
		{
			field: "a[x].b[1].c",
			want:  "a.x.b.1.c",
		},
		{
			field: "a[x.y]",
			want:  "a.x.y",
		},
		{
			field: "a[x[y]]",
			want:  "a.x.y",
		},
		{
			field: "a[x]y]",
			want:  "a.xy",
		},
	} {
		t.Run(tc.field, func(t *testing.T) {
			original := validation.Invalid(tc.field, "required", "", foreign)
			got := validation.DottedPaths(original)
			var failure *validation.Error
			if !errors.As(got, &failure) || !errors.Is(got, foreign) || failure.Violations[0].Field != tc.want {
				t.Fatalf("converted: %v", got)
			}
			failure.Violations[0].Field = "changed"
			var before *validation.Error
			if !errors.As(original, &before) || before.Violations[0].Field != tc.field {
				t.Fatal("original was mutated")
			}
		})
	}
}

func TestDottedPathsAmbiguousMapKeys(t *testing.T) {
	type config struct {
		Values map[string]string `k:"values" validate:"dive,required"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	for _, tc := range []struct {
		key  string
		want string
	}{
		{
			key:  "x.y",
			want: "values.x.y",
		},
		{
			key:  "x[y",
			want: "values.x.y",
		},
		{
			key:  "x]y",
			want: "values.xy",
		},
	} {
		t.Run(tc.key, func(t *testing.T) {
			cfg := config{
				Values: map[string]string{
					tc.key: "",
				},
			}
			var failure *validation.Error
			if !errors.As(validation.DottedPaths(validation.FromStruct(cfg, v.Struct(cfg))), &failure) ||
				len(failure.Violations) != 1 || failure.Violations[0].Field != tc.want {
				t.Fatalf("converted: %#v", failure)
			}
		})
	}
}
