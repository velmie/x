// Package apperror describes application failures independently of transport.
//
// Code, Message and Violations are public descriptions owned by the application.
// Transports must not expose Error or Cause as a client message. An Error and its
// Violations slice must not be mutated after publication.
package apperror

import (
	"fmt"
	"strings"
)

const (
	// RuleRequired identifies a required value.
	RuleRequired = "required"
	// RuleMin identifies a minimum.
	RuleMin = "min"
	// RuleMax identifies a maximum.
	RuleMax = "max"
	// RuleGT identifies a strict lower bound.
	RuleGT = "gt"
	// RuleEQ identifies an equality constraint.
	RuleEQ = "eq"
	// RuleGTE identifies an inclusive lower bound.
	RuleGTE = "gte"
	// RuleUnique identifies a uniqueness constraint.
	RuleUnique = "unique"
	// RuleLTEField identifies an upper bound from another field.
	RuleLTEField = "ltefield"
	// RuleType identifies an expected input type.
	RuleType = "type"
)

// Category identifies a closed class of application failure.
type Category uint8

const (
	// Internal identifies an unexpected failure.
	Internal Category = iota
	// Invalid identifies invalid operation input.
	Invalid
	// Forbidden identifies denied authorization.
	Forbidden
	// NotFound identifies an unavailable requested resource.
	NotFound
	// Conflict identifies conflicting state or concurrent modification.
	Conflict
	// Precondition identifies an unmet operation requirement.
	Precondition
	// Unavailable identifies a currently unavailable dependency.
	Unavailable
)

// String returns the category name, or "unknown(N)" for an unsupported value.
func (c Category) String() string {
	switch c {
	case Internal:
		return "internal"
	case Invalid:
		return "invalid"
	case Forbidden:
		return "forbidden"
	case NotFound:
		return "not_found"
	case Conflict:
		return "conflict"
	case Precondition:
		return "precondition"
	case Unavailable:
		return "unavailable"
	default:
		return fmt.Sprintf("unknown(%d)", c)
	}
}

// Error carries a public classification and an optional diagnostic cause.
// The zero value is Internal. Non-internal errors require a nonempty Code.
// Error and its Violations must not be mutated after they are published.
type Error struct {
	Category   Category
	Code       string
	Message    string
	Violations []Violation
	Cause      error
}

// Error describes the failure, its violated fields, and its underlying cause.
// This diagnostic text may contain sensitive information and is not a public message.
func (e *Error) Error() string {
	message := e.Message
	if message == "" {
		message = e.Code
	}
	if message == "" {
		message = e.Category.String()
	}
	var b strings.Builder
	b.WriteString(message)
	for i, v := range e.Violations {
		if i == 0 {
			b.WriteString(": ")
		} else {
			b.WriteString(", ")
		}
		b.WriteString(v.Field)
		if v.Rule != "" {
			b.WriteString(" (")
			b.WriteString(v.Rule)
			if v.Param != "" {
				b.WriteByte('=')
				b.WriteString(v.Param)
			}
			b.WriteByte(')')
		} else if v.Param != "" {
			b.WriteString(" (")
			b.WriteString(v.Param)
			b.WriteByte(')')
		}
		if v.Message != "" {
			b.WriteString(": ")
			b.WriteString(v.Message)
		}
	}
	if e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

// Unwrap returns the original diagnostic cause.
func (e *Error) Unwrap() error { return e.Cause }

// Violation identifies a public field constraint without its rejected value.
// Field is a public DTO path and Rule is its owning rule's identifier. Param and
// Message are optional. Applications must supply only values safe for clients.
type Violation struct {
	Field   string
	Rule    string
	Param   string
	Message string
}
