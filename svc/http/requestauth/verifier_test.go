package requestauth_test

import (
	"testing"

	. "github.com/velmie/x/svc/http/requestauth"
)

func TestEqString(t *testing.T) {
	tests := []struct {
		value    any
		expected bool
	}{
		{"test", true},
		{123, false},
		{nil, false},
		{"wrong", false},
	}

	verifier := EqString("test")

	for _, test := range tests {
		_, result := verifier(test.value)
		if result != test.expected {
			t.Errorf("expected %v, got %v for value %v", test.expected, result, test.value)
		}
	}
}

func TestEmptyString(t *testing.T) {
	tests := []struct {
		value    any
		expected bool
	}{
		{"", true},
		{"test", false},
		{123, false},
	}

	verifier := EmptyString()

	for _, test := range tests {
		_, result := verifier(test.value)
		if result != test.expected {
			t.Errorf("expected %v, got %v for value %v", test.expected, result, test.value)
		}
	}
}

func TestNot(t *testing.T) {
	alwaysTrue := func(value any) (error, bool) {
		return nil, true
	}
	alwaysFalse := func(value any) (error, bool) {
		return nil, false
	}

	verifier := Not(alwaysTrue)
	_, result := verifier(nil)
	if result {
		t.Error("expected false for Not with alwaysTrue verifier")
	}

	verifier = Not(alwaysFalse)
	_, result = verifier(nil)
	if !result {
		t.Error("expected true for Not with alwaysFalse verifier")
	}
}
