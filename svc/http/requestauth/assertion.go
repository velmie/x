package requestauth

import "fmt"

type Assertion struct {
	Description string
	Verify      Verifier
	Claim       string
	Required    bool
}

func (a *Assertion) assert(entity Entity) (error, bool) {
	value, ok := entity[a.Claim]
	if !ok {
		if a.Required {
			return fmt.Errorf("'%s' is required: %w", a.Claim, ErrRequired), false
		}
	}
	return a.Verify(value)
}

// Verifier verifies actual value
type Verifier func(value any) (error, bool)

func VerifyRequired(claim string, verify Verifier, description string) *Assertion {
	return assert(claim, true, verify, description)
}

func Verify(claim string, verify Verifier, description string) *Assertion {
	return assert(claim, false, verify, description)
}

func assert(claim string, required bool, verify Verifier, description string) *Assertion {
	return &Assertion{
		Description: description,
		Verify:      verify,
		Claim:       claim,
		Required:    required,
	}
}
