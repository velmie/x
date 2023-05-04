package authentication

import (
	"context"
	"crypto"
)

// KeySource is used in order to fetch publicDeriver key by the given kid (key id)
type KeySource interface {
	FetchPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error)
}

// KeySourceFunc is a function that implements KeySource interface
type KeySourceFunc func(ctx context.Context, kid string) (crypto.PublicKey, error)

func (f KeySourceFunc) FetchPublicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	return f(ctx, kid)
}

// KeySourceMap is a map of kid (key id) to publicDeriver key
type KeySourceMap map[string]crypto.PublicKey

// FetchPublicKey fetches publicDeriver key by the given kid (key id)
func (m KeySourceMap) FetchPublicKey(_ context.Context, kid string) (crypto.PublicKey, error) {
	publicKey, ok := m[kid]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return publicKey, nil
}

// KeySourceSingle is a KeySource that returns a single publicDeriver key
type KeySourceSingle struct {
	PublicKey crypto.PublicKey
}

func (k KeySourceSingle) FetchPublicKey(_ context.Context, _ string) (crypto.PublicKey, error) {
	return k.PublicKey, nil
}
