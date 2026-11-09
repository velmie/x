// Package validation provides declarative validation and strict decoding.
// DecodeQuery binds flat URL queries to tagged DTOs and validates them using
// a caller-configured validator. FromStruct converts validator failures into
// field diagnostics, Error collects checks, Invalid reports one plain-Go check,
// and At prefixes paths.
package validation

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

const (
	// RuleNonzeroCanonicalUUID identifies nonzero lowercase canonical UUID text.
	RuleNonzeroCanonicalUUID = "nonzero_canonical_uuid"
	// RuleUTF8 identifies valid UTF-8 string content.
	RuleUTF8 = "utf8"
	// RuleMaxBytes identifies a maximum string byte length.
	RuleMaxBytes = "maxbytes"
	// RuleNonblank identifies a string containing non-whitespace content.
	RuleNonblank = "nonblank"
	// RuleTrimmedNonblank identifies nonempty text without surrounding whitespace.
	RuleTrimmedNonblank = "trimmed_nonblank"
)

// Register installs the five shared string rules on a caller-owned validator.
// Complete registration before using the validator, including concurrent use.
func Register(v *validator.Validate) error {
	for _, rule := range []struct {
		name     string
		validate validator.Func
	}{
		{
			name: RuleNonzeroCanonicalUUID,
			validate: func(field validator.FieldLevel) bool {
				return NonzeroCanonicalUUID(field.Field().String())
			},
		},
		{
			name: RuleUTF8,
			validate: func(field validator.FieldLevel) bool {
				return utf8.ValidString(field.Field().String())
			},
		},
		{
			name: RuleMaxBytes,
			validate: func(field validator.FieldLevel) bool {
				maximum, err := strconv.Atoi(field.Param())
				return err == nil && maximum >= 0 && len(field.Field().String()) <= maximum
			},
		},
		{
			name: RuleNonblank,
			validate: func(field validator.FieldLevel) bool {
				return strings.TrimSpace(field.Field().String()) != ""
			},
		},
		{
			name: RuleTrimmedNonblank,
			validate: func(field validator.FieldLevel) bool {
				return TrimmedNonblank(field.Field().String())
			},
		},
	} {
		if err := v.RegisterValidation(rule.name, rule.validate); err != nil {
			return fmt.Errorf("register validation rule %s: %w", rule.name, err)
		}
	}
	return nil
}

// TrimmedNonblank reports whether value is nonempty and has no surrounding whitespace.
func TrimmedNonblank(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

// NonzeroCanonicalUUID accepts nonzero, lowercase hexadecimal UUID text in the
// 8-4-4-4-12 hyphenated format. It does not restrict the version or variant.
func NonzeroCanonicalUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	nonzero := false
	for i := range len(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if value[i] != '-' {
				return false
			}
			continue
		}
		c := value[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}
