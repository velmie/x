package authentication

import "crypto"

// KeySource is used in order to fetch publicDeriver key by the given kid (key id)
type KeySource interface {
	FetchPublicKey(kid string) (crypto.PublicKey, error)
}

// KeySourceFunc is a function that implements KeySource interface
type KeySourceFunc func(kid string) (crypto.PublicKey, error)

func (f KeySourceFunc) FetchPublicKey(kid string) (crypto.PublicKey, error) {
	return f(kid)
}

// KeySourceMap is a map of kid (key id) to publicDeriver key
type KeySourceMap map[string]crypto.PublicKey

// FetchPublicKey fetches publicDeriver key by the given kid (key id)
func (m KeySourceMap) FetchPublicKey(kid string) (crypto.PublicKey, error) {
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

func (k KeySourceSingle) FetchPublicKey(_ string) (crypto.PublicKey, error) {
	return k.PublicKey, nil
}
