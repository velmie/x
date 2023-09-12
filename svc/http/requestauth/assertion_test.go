package requestauth

import (
	"errors"
	"testing"
)

func TestAssertionAssert(t *testing.T) {
	tests := []struct {
		description string
		assertion   *Assertion
		entity      Entity
		expected    bool
		err         error
	}{
		{
			description: "should confirm, no errors",
			assertion:   Verify("name", EqString("test"), "name test"),
			entity:      Entity{"name": "test"},
			expected:    true,
			err:         nil,
		},
		{
			description: "should not confirm, no errors",
			assertion:   Verify("name", EqString("test"), "name test"),
			entity:      Entity{"name": "wrong"},
			expected:    false,
			err:         nil,
		},
		{
			description: "missing required field, should return error",
			assertion:   VerifyRequired("name", EqString("test"), "name test"),
			entity:      Entity{},
			expected:    false,
			err:         ErrRequired,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			err, result := test.assertion.assert(test.entity)
			if result != test.expected || !errors.Is(err, test.err) {
				t.Errorf("expected result %v with error %v, got %v with error %v", test.expected, test.err, result, err)
			}
		})
	}
}
