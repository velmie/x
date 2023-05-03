package authentication

import (
	"errors"
	"fmt"
)

// JSONWebToken represents a parsed JWT
type JSONWebToken struct {
	Raw       string         // Raw contains the raw token
	Header    map[string]any // Header is the first segment of the token in decoded form
	Claims    map[string]any // Claims is the second segment of the token in decoded form
	Signature []byte         // Signature is the third segment of the token in decoded form
	Valid     bool           // Valid specifies if the token is valid
}

type JWTParser interface {
	Parse(token string, keySource KeySource) (*JSONWebToken, error)
}

// ViaJWT is used in order to authenticate entity by the given JWT
type ViaJWT struct {
	keySource KeySource
	parser    JWTParser
	hook      func(token *JSONWebToken) error
}

// ViaJWTOption is used in order to configure ViaJWT
type ViaJWTOption func(v *ViaJWT)

// NewViaJWT is used in order to create new ViaJWT instance
func NewViaJWT(parser JWTParser, keySource KeySource, options ...ViaJWTOption) *ViaJWT {
	v := &ViaJWT{keySource: keySource, parser: parser}
	for _, option := range options {
		option(v)
	}
	return v
}

// Authenticate is used in order to authenticate entity by the given token
func (v *ViaJWT) Authenticate(token string) (Entity, error) {
	parsedToken, err := v.parser.Parse(token, v.keySource)
	if err != nil {
		if errors.Is(err, ErrTokenUnverifiable) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	if v.hook != nil {
		if err = v.hook(parsedToken); err != nil {
			return nil, err
		}
	}
	if !parsedToken.Valid {
		return nil, ErrNotAuthenticated
	}
	return parsedToken.Claims, nil
}

// JWTWithHook is used in order to set custom verification function
func JWTWithHook(hook func(token *JSONWebToken) error) ViaJWTOption {
	return func(v *ViaJWT) {
		v.hook = hook
	}
}
