// Package identity defines authenticated callers and authentication outcomes
// shared by service boundaries. It does not parse credentials or decide
// whether a caller may perform an application operation.
package identity

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	// MaxIssuerBytes is the maximum encoded issuer length.
	MaxIssuerBytes = 512
	// MaxSubjectBytes is the maximum encoded subject length.
	MaxSubjectBytes = 128
	// MaxPermissions is the maximum number of supplied permissions.
	MaxPermissions = 32
	// MaxPermissionBytes is the maximum encoded permission length.
	MaxPermissionBytes = 64
)

var (
	// ErrInvalidCredentials identifies credentials that cannot establish identity.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUnavailable identifies an authentication dependency failure.
	ErrUnavailable = errors.New("authentication dependency unavailable")
)

// Principal identifies a caller and its exact permissions. It does not record
// authentication provenance. The establishing verifier validates permissions.
type Principal struct {
	Issuer      string
	Subject     string
	Permissions []string
}

// Valid reports whether the issuer and subject satisfy identity bounds. It does
// not validate permissions or establish authentication provenance.
func (p Principal) Valid() bool {
	return ValidIssuer(p.Issuer) && ValidSubject(p.Subject)
}

// Has reports whether Permissions contains permission, using exact comparison.
func (p Principal) Has(permission string) bool {
	return slices.Contains(p.Permissions, permission)
}

// ValidIssuer accepts nonblank UTF-8 issuer text within MaxIssuerBytes.
func ValidIssuer(issuer string) bool {
	return validText(issuer, MaxIssuerBytes)
}

// ValidSubject accepts nonblank UTF-8 subject text within MaxSubjectBytes.
func ValidSubject(subject string) bool {
	return validText(subject, MaxSubjectBytes)
}

func validText(value string, maxBytes int) bool {
	return utf8.ValidString(value) && len(value) <= maxBytes && strings.TrimSpace(value) != ""
}
