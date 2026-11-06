package authentication

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"math/big"
)

// MaxStaticJWKSBytes bounds an in-memory JWKS document.
const MaxStaticJWKSBytes = 64 << 10

// StaticJWKS is an immutable public verification-key source. It owns no
// background work. Returned public keys are copies and may be used independently.
type StaticJWKS struct {
	snapshot *jwksSnapshot
}

// NewStaticJWKS validates a nonempty public JWKS without starting background
// work. Every entry must be eligible under policy; optional metadata follows the
// same rules as managed JWKS. Duplicate IDs are always rejected before filtering
// key usage. The zero key ID policy accepts absent IDs without a byte limit.
// The supplied bytes and policies are not retained.
func NewStaticJWKS(data []byte, policy JWKSVerificationPolicy, keyIDs JWKSKeyIDPolicy) (*StaticJWKS, error) {
	if len(data) > MaxStaticJWKSBytes {
		return nil, rejectedJWK("read", "document_too_large", "keys", nil)
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	if err := keyIDs.validate(); err != nil {
		return nil, err
	}
	snapshot, err := decodeJWKSDocument(data, &policy, keyIDs, true)
	if err != nil {
		return nil, err
	}
	if len(snapshot.keys) == 0 || len(snapshot.rejected) != 0 {
		return nil, rejectedJWK("validate", "not_all_verification_keys", "keys", nil)
	}
	return &StaticJWKS{
		snapshot: snapshot,
	}, nil
}

// FetchPublicKey returns the public key identified by kid. A missing key matches
// ErrKeyNotFound. Use FetchPublicKeyForAlgorithm when verifying JWTs.
func (s *StaticJWKS) FetchPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	return s.fetchPublicKey(ctx, kid, "")
}

// FetchPublicKeyForAlgorithm also enforces the key's algorithm binding. A
// mismatch matches both ErrKeyRejected and ErrJWKSKeyRejected.
func (s *StaticJWKS) FetchPublicKeyForAlgorithm(
	ctx context.Context,
	kid string,
	algorithm string,
) (crypto.PublicKey, error) {
	if algorithm == "" {
		return nil, rejectedKey("missing_algorithm", "header.alg")
	}
	return s.fetchPublicKey(ctx, kid, algorithm)
}

func (s *StaticJWKS) fetchPublicKey(ctx context.Context, kid, algorithm string) (crypto.PublicKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, found := s.snapshot.keys[kid]
	if !found {
		return nil, ErrKeyNotFound
	}
	if algorithm != "" && s.snapshot.algorithms[kid] != algorithm {
		return nil, rejectedKey("algorithm_mismatch", "header.alg")
	}
	switch key := key.(type) {
	case *rsa.PublicKey:
		return &rsa.PublicKey{
			N: new(big.Int).Set(key.N),
			E: key.E,
		}, nil
	case *ecdsa.PublicKey:
		return &ecdsa.PublicKey{
			Curve: key.Curve,
			X:     new(big.Int).Set(key.X),
			Y:     new(big.Int).Set(key.Y),
		}, nil
	case ed25519.PublicKey:
		return append(ed25519.PublicKey(nil), key...), nil
	default:
		panic("authentication: validated JWKS contains an unsupported key")
	}
}
