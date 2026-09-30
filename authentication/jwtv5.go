package authentication

import (
	"context"
	"crypto"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

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
		var publicKey any
		var err error
		if source, ok := keySource.(interface {
			FetchPublicKeyForAlgorithm(context.Context, string, string) (crypto.PublicKey, error)
		}); ok {
			publicKey, err = source.FetchPublicKeyForAlgorithm(ctx, kid, token.Method.Alg())
		} else {
			publicKey, err = keySource.FetchPublicKey(ctx, kid)
		}
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil, fmt.Errorf("key is not found: %w: %w", ErrTokenUnverifiable, err)
			}
			return nil, fmt.Errorf("failed to fetch public key: %w", err)
		}
		return publicKey, nil
	})

	if err != nil {
		if errors.Is(err, ErrTokenUnverifiable) {
			return nil, err
		}
		return nil, &classifiedError{category: classifyJWTError(err), cause: err}
	}

	return &JSONWebToken{
		Raw:       parsedToken.Raw,
		Header:    parsedToken.Header,
		Claims:    parsedToken.Claims.(jwt.MapClaims),
		Signature: parsedToken.Signature,
		Valid:     parsedToken.Valid,
	}, nil
}

// classifyJWTError gives parsing and verification failures priority over claims
// failures, including when an upstream error contains several causes.
func classifyJWTError(err error) Error {
	switch {
	case errors.Is(err, jwt.ErrInvalidKey),
		errors.Is(err, jwt.ErrInvalidKeyType),
		errors.Is(err, jwt.ErrHashUnavailable),
		errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenUnverifiable),
		errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrSignatureInvalid),
		errors.Is(err, jwt.ErrInvalidType):
		return ErrBadToken
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing),
		errors.Is(err, jwt.ErrTokenInvalidAudience),
		errors.Is(err, jwt.ErrTokenExpired),
		errors.Is(err, jwt.ErrTokenUsedBeforeIssued),
		errors.Is(err, jwt.ErrTokenInvalidIssuer),
		errors.Is(err, jwt.ErrTokenInvalidSubject),
		errors.Is(err, jwt.ErrTokenNotValidYet),
		errors.Is(err, jwt.ErrTokenInvalidId),
		errors.Is(err, jwt.ErrTokenInvalidClaims):
		return ErrNotAuthenticated
	default:
		return ErrBadToken
	}
}
