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

func TestConfigurationErrors(t *testing.T) {
	type child struct {
		Name string `k:"name" validate:"required"`
	}
	type config struct {
		Nested child            `k:"nested"`
		Items  []child          `k:"items" validate:"dive"`
		Values map[string]child `k:"values" validate:"dive"`
		Limit  int              `k:"limit" validate:"gte=2"`
	}
	cfg := config{
		Items:  []child{{}},
		Values: map[string]child{"key": {}},
	}
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(validation.FieldNames("k"))
	cause := v.Struct(cfg)
	got := validation.FromStruct(cfg, cause)
	var failure *validation.Error
	if !errors.As(got, &failure) {
		t.Fatalf("expected validation error: %v", got)
	}
	want := []apperror.Violation{
		{
			Field: "nested.name",
			Rule:  "required",
		},
		{
			Field: "items[0].name",
			Rule:  "required",
		},
		{
			Field: "values[key].name",
			Rule:  "required",
		},
		{
			Field: "limit",
			Rule:  "gte",
			Param: "2",
		},
	}
	if !reflect.DeepEqual(failure.Violations, want) {
		t.Fatalf("violations = %#v", failure.Violations)
	}
	var native validator.ValidationErrors
	if !errors.As(got, &native) {
		t.Fatalf("cause not retained: %v", got)
	}
	if !strings.Contains(got.Error(), "limit: gte=2") || !strings.Contains(got.Error(), strings.ReplaceAll(cause.Error(), "\n", "; ")) {
		t.Fatalf("error text: %q", got)
	}
	prefixed := validation.At("a", validation.At("b", got))
	var nested *validation.Error
	if !errors.As(prefixed, &nested) || nested.Violations[0].Field != "a.b.nested.name" || failure.Violations[0].Field != "nested.name" {
		t.Fatalf("prefix/immutability: %#v %#v", nested, failure)
	}
	if !errors.As(prefixed, &native) {
		t.Fatal("prefix lost original cause")
	}
	plain := errors.New("cross-field conflict")
	invalid := validation.Invalid("tls", apperror.RuleEQ, "", plain)
	if !errors.Is(invalid, plain) || !strings.Contains(invalid.Error(), "tls: eq") || !strings.Contains(invalid.Error(), plain.Error()) {
		t.Fatalf("invalid: %v", invalid)
	}
	if validation.FromStruct(cfg, nil) != nil || validation.At("x", nil) != nil {
		t.Fatal("nil not preserved")
	}
	if !errors.Is(validation.FromStruct(cfg, plain), plain) || validation.At("x", plain) != plain {
		t.Fatal("ordinary error changed")
	}
	wrapped := fmt.Errorf("outer: %w", got)
	if validation.At("x", wrapped) != wrapped {
		t.Fatal("At changed a wrapped error")
	}
	wrappedNative := fmt.Errorf("outer: %w", cause)
	if !errors.Is(validation.FromStruct(cfg, wrappedNative), wrappedNative) {
		t.Fatal("FromStruct changed a wrapped error")
	}
	invalidType := v.Struct(42)
	if !errors.Is(validation.FromStruct(cfg, invalidType), invalidType) {
		t.Fatal("invalid validation error changed")
	}
}
