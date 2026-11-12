package validation_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
	"github.com/velmie/x/svc/apperror"

	"github.com/velmie/x/svc/validation"
)

func TestErrorCollector(t *testing.T) {
	var failures validation.Error
	if err := failures.Err(); err != nil {
		t.Fatalf("zero collector returned %T: %v", err, err)
	}
	failures.AddStruct(nil, nil)
	if err := failures.Err(); err != nil {
		t.Fatalf("nil input changed collector: %v", err)
	}
	first := errors.New("first cause")
	second := errors.New("second cause")
	failures.Add("host", apperror.RuleRequired, "", nil)
	failures.Add("port", apperror.RuleGT, "0", first)
	failures.Add("mode", apperror.RuleEQ, "tls", second)
	err := failures.Err()
	if err != &failures || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("collector lost identity or causes: %v", err)
	}
	for _, want := range []string{"host: required", "port: gt=0", "mode: eq=tls", "first cause", "second cause"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
	for _, violation := range failures.Violations {
		if violation.Message != "" {
			t.Errorf("unexpected message: %+v", violation)
		}
	}
}

func TestErrorMerge(t *testing.T) {
	type child struct {
		Name string `k:"name" validate:"required"`
	}
	type config struct {
		Items []child `k:"items" validate:"dive"`
	}
	cfg := config{
		Items: []child{
			{},
		},
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	native := v.Struct(cfg)
	first := errors.New("first")
	foreign := errors.New("foreign")
	var merged validation.Error
	merged.Merge(nil)
	if merged.Err() != nil {
		t.Fatal("nil merge changed empty collector")
	}
	merged.Merge(validation.Invalid("manual", "required", "", first))
	merged.Merge(validation.At("cfg", validation.DottedPaths(validation.FromStruct(cfg, native))))
	wrapped := fmt.Errorf("wrapped: %w", validation.Invalid("ignored", "required", "", nil))
	merged.Merge(wrapped)
	merged.Merge(foreign)
	want := []apperror.Violation{
		{
			Field: "manual",
			Rule:  "required",
		},
		{
			Field: "cfg.items.0.name",
			Rule:  "required",
		},
	}
	if !reflect.DeepEqual(merged.Violations, want) {
		t.Fatalf("violations = %#v", merged.Violations)
	}
	var failures validator.ValidationErrors
	if merged.Err() != &merged || !errors.Is(merged.Err(), first) || !errors.Is(merged.Err(), foreign) ||
		!errors.Is(merged.Err(), wrapped) || !errors.As(merged.Err(), &failures) {
		t.Fatalf("causes lost: %v", merged.Err())
	}
	var copied *validation.Error
	if !errors.As(validation.DottedPaths(validation.At("root", merged.Err())), &copied) ||
		copied.Violations[1].Field != "root.cfg.items.0.name" {
		t.Fatalf("composition failed: %#v", copied)
	}
}

func TestCollectorAddStruct(t *testing.T) {
	type config struct {
		Name string `k:"name" validate:"required"`
	}
	v := validator.New()
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	cfg := config{}
	cause := v.Struct(cfg)
	var failures validation.Error
	failures.AddStruct(cfg, cause)
	var native validator.ValidationErrors
	if len(failures.Violations) != 1 || failures.Violations[0].Field != "name" || !errors.As(failures.Err(), &native) {
		t.Fatalf("native validation lost: %v", failures.Err())
	}
	bad := v.Struct(123)
	var invalid *validator.InvalidValidationError
	failures.AddStruct(cfg, bad)
	if !errors.As(failures.Err(), &invalid) || !errors.Is(failures.Err(), bad) {
		t.Fatalf("invalid validation cause lost: %v", failures.Err())
	}
	var onlyCause validation.Error
	onlyCause.AddStruct(cfg, bad)
	if onlyCause.Err() == nil || len(onlyCause.Violations) != 0 || !errors.Is(onlyCause.Err(), bad) {
		t.Fatalf("cause-only collector failed: %v", onlyCause.Err())
	}
	var converted *validation.Error
	if err := validation.FromStruct(cfg, bad); !errors.As(err, &converted) || !errors.Is(err, bad) {
		t.Fatalf("FromStruct lost invalid cause: %v", err)
	}
}

func TestInvalidShorthand(t *testing.T) {
	cause := errors.New("conflict")
	err := validation.Invalid("tls", apperror.RuleEQ, "false", cause)
	var failure *validation.Error
	if !errors.As(err, &failure) || len(failure.Violations) != 1 || !errors.Is(err, cause) ||
		failure.Violations[0].Field != "tls" || failure.Violations[0].Param != "false" {
		t.Fatalf("invalid shorthand: %v", err)
	}
}
