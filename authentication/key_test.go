package authentication_test

import (
	"context"
	"crypto"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/velmie/x/authentication"
)

func TestKeySourceFunc(t *testing.T) {
	key := crypto.PublicKey(&struct{}{})
	kid := "testKey"

	keySourceFunc := func(ctx context.Context, kid string) (crypto.PublicKey, error) {
		if kid == "testKey" {
			return key, nil
		}
		return nil, errors.New("key not found")
	}
	ctx := context.Background()

	ks := authentication.KeySourceFunc(keySourceFunc)
	result, err := ks.FetchPublicKey(ctx, kid)
	assert.NoError(t, err)
	assert.Equal(t, key, result)
}

func TestKeySourceMap(t *testing.T) {
	key := crypto.PublicKey(&struct{}{})
	kid := "testKey"

	ks := authentication.KeySourceMap{
		kid: key,
	}
	ctx := context.Background()

	t.Run("ExistingKey", func(t *testing.T) {
		result, err := ks.FetchPublicKey(ctx, kid)
		assert.NoError(t, err)
		assert.Equal(t, key, result)
	})

	t.Run("NonExistingKey", func(t *testing.T) {
		_, err := ks.FetchPublicKey(ctx, "nonExisting")
		assert.Error(t, err)
		assert.Equal(t, authentication.ErrKeyNotFound, err)
	})
}

func TestSingleKeySource(t *testing.T) {
	key := crypto.PublicKey(&struct{}{})

	ks := authentication.KeySourceSingle{
		PublicKey: key,
	}
	ctx := context.Background()

	t.Run("FetchPublicKey", func(t *testing.T) {
		result, err := ks.FetchPublicKey(ctx, "")
		assert.NoError(t, err)
		assert.Equal(t, key, result)
	})
}
