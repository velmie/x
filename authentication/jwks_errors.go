package authentication

import (
	"errors"
	"fmt"
)

// JWKSError describes a JWKS failure or warning using bounded diagnostic fields.
// Error includes the text of the primary and any secondary close cause.
// The causes remain available to errors.Is and errors.As. DiagnosticFunc receives these events.
type JWKSError struct {
	operation  string
	stage      string
	reason     string
	field      string
	outcome    string
	status     int
	cause      error
	closeCause error
}

// Operation identifies the operation: jwks_config, jwks_lookup, or jwks_refresh.
func (e *JWKSError) Operation() string {
	if e.operation != "" {
		return e.operation
	}
	switch e.stage {
	case "config":
		return "jwks_config"
	case "lookup":
		return "jwks_lookup"
	default:
		return "jwks_refresh"
	}
}

// Stage identifies where the operation failed, for example request or decode.
func (e *JWKSError) Stage() string { return e.stage }

// Reason returns a stable reason code rather than upstream error text.
func (e *JWKSError) Reason() string { return e.reason }

// Field identifies the affected configuration or JSON field, for example
// RefreshInterval or keys[1].alg. It contains schema names and array indices,
// never input values. An empty string means no individual field is implicated.
func (e *JWKSError) Field() string { return e.field }

// Outcome is failed, warning, or defaulted (an invalid legacy option was reset).
func (e *JWKSError) Outcome() string {
	if e.outcome != "" {
		return e.outcome
	}
	return "failed"
}

// HTTPStatus returns the rejected HTTP status, or zero for other failures.
func (e *JWKSError) HTTPStatus() int { return e.status }

// Error describes the operation, failure, and any primary or close cause.
func (e *JWKSError) Error() string {
	message := "operation=" + e.Operation() + " stage=" + e.Stage() + " reason=" + e.Reason() + " outcome=" + e.Outcome()
	if e.status != 0 {
		message += fmt.Sprintf(" status=%d", e.status)
	}
	if e.field != "" {
		message += " field=" + e.field
	}
	if e.closeCause != nil {
		message += " close_reason=body_close_failed"
	}
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}
	if e.closeCause != nil {
		message += "; close: " + e.closeCause.Error()
	}
	return message
}

// Unwrap exposes the primary and any secondary close cause to errors.Is and errors.As.
// Their text is included in Error.
func (e *JWKSError) Unwrap() error {
	if e.closeCause != nil {
		return errors.Join(e.cause, e.closeCause)
	}
	return e.cause
}
