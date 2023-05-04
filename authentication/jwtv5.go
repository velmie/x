package authentication

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// jwtErrMap maps known errors from jwt package to local errors
var jwtErrMap = map[error]Error{
	jwt.ErrInvalidKey:                ErrBadToken,
	jwt.ErrInvalidKeyType:            ErrBadToken,
	jwt.ErrHashUnavailable:           ErrBadToken,
	jwt.ErrTokenMalformed:            ErrBadToken,
	jwt.ErrTokenUnverifiable:         ErrBadToken,
	jwt.ErrTokenSignatureInvalid:     ErrBadToken,
	jwt.ErrSignatureInvalid:          ErrBadToken,
	jwt.ErrTokenRequiredClaimMissing: ErrNotAuthenticated,
	jwt.ErrTokenInvalidAudience:      ErrNotAuthenticated,
	jwt.ErrTokenExpired:              ErrNotAuthenticated,
	jwt.ErrTokenUsedBeforeIssued:     ErrNotAuthenticated,
	jwt.ErrTokenInvalidIssuer:        ErrNotAuthenticated,
	jwt.ErrTokenInvalidSubject:       ErrNotAuthenticated,
	jwt.ErrTokenNotValidYet:          ErrNotAuthenticated,
	jwt.ErrTokenInvalidId:            ErrNotAuthenticated,
	jwt.ErrTokenInvalidClaims:        ErrNotAuthenticated,
	jwt.ErrInvalidType:               ErrBadToken,
}

// JWTv5Parser is a wrapper around jwt.Parser
type JWTv5Parser struct {
	parser *jwt.Parser
}

// NewJWTv5Parser creates a new JWTv5Parser
func NewJWTv5Parser(parser *jwt.Parser) *JWTv5Parser {
	return &JWTv5Parser{parser: parser}
}

// Parse parses a given token and returns a JSONWebToken
func (p *JWTv5Parser) Parse(ctx context.Context, token string, keySource KeySource) (*JSONWebToken, error) {
	parsedToken, err := p.parser.Parse(token, func(token *jwt.Token) (any, error) {
		kid := ""
		if kidClaim, ok := token.Header["kid"]; ok {
			if kid, ok = kidClaim.(string); !ok {
				return nil, fmt.Errorf(
					"invalid 'kid' claim type: %T, the 'kid' claim must be of string type: %w",
					kidClaim,
					ErrBadToken,
				)
			}
		}
		publicKey, err := keySource.FetchPublicKey(ctx, kid)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, fmt.Errorf("key is not found: %w", ErrTokenUnverifiable)
			}
			return nil, fmt.Errorf("failed to fetch public key: %w", err)
		}
		return publicKey, nil
	})

	if err != nil {
		if errors.Is(err, ErrTokenUnverifiable) {
			return nil, err
		}
		if knownErr, ok := jwtErrMap[errors.Unwrap(err)]; ok {
			return nil, fmt.Errorf("%w: %v", knownErr, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrBadToken, err)
	}

	return &JSONWebToken{
		Raw:       parsedToken.Raw,
		Header:    parsedToken.Header,
		Claims:    parsedToken.Claims.(jwt.MapClaims),
		Signature: parsedToken.Signature,
		Valid:     parsedToken.Valid,
	}, nil
}
