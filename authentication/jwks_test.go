package authentication_test

import (
	"crypto/ecdsa"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/velmie/x/authentication"
)

const (
	jwksJSON = `{
    "keys": [
        {
            "use": "sig",
            "kty": "EC",
            "kid": "test-kid",
            "crv": "P-256",
            "alg": "ES256",
            "x": "6705KrnpI-OzekE4hmzj4CBRas8nXEkffye7VNwAHAY",
            "y": "yt0olv9aYpPbupqXSqlxfQ4tfxD4sr_5unefPMjr3Bw"
        }
    ]
}`
)

type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

func TestKeySourceJWKS_FetchPublicKey(t *testing.T) {
	client := &MockHTTPClient{}
	body := strings.NewReader(jwksJSON)

	client.On("Do", mock.Anything).Return(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(body),
	}, nil).Twice()

	keySource := createKeySourceJWKS(&authentication.JWKSOptions{
		Client:              client,
		RequestOnUnknownKID: true, // this must force the client to make a request
	},
	)

	t.Run("successful fetch", func(t *testing.T) {
		_, _ = body.Seek(0, io.SeekStart)
		key, err := keySource.FetchPublicKey("test-kid")
		assert.NoError(t, err)
		assert.NotNil(t, key)
		assert.IsType(t, &ecdsa.PublicKey{}, key)
	})

	t.Run("unknown key", func(t *testing.T) {
		_, _ = body.Seek(0, io.SeekStart)
		key, err := keySource.FetchPublicKey("unknown-kid")
		assert.Equal(t, authentication.ErrKeyNotFound, err)
		assert.Nil(t, key)
	})

	t.Run("do not request on unknown kid", func(t *testing.T) {
		client2 := &MockHTTPClient{}
		client2.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(body),
		}, nil).Once()

		keySource2 := createKeySourceJWKS(&authentication.JWKSOptions{
			Client:              client2,
			RequestOnUnknownKID: false,
		})

		for i := 0; i < 5; i++ {
			_, _ = body.Seek(0, io.SeekStart)
			key, err := keySource2.FetchPublicKey("unknown-kid")
			assert.Equal(t, authentication.ErrKeyNotFound, err)
			assert.Nil(t, key)
		}
	})

	t.Run("request rate limit and warning", func(t *testing.T) {
		_, _ = body.Seek(0, io.SeekStart)
		client2 := &MockHTTPClient{}
		client2.On("Do", mock.Anything).Return(&http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(body),
		}, nil).Once()

		warnCalled := false
		options := &authentication.JWKSOptions{
			Client:              client2,
			RequestOnUnknownKID: true,
			WarnFunc: func(msg string) {
				assert.Contains(t, msg, "rate limit")
				warnCalled = true
			},
		}
		options.SetRefreshRateLimit(1, time.Minute)
		keySource2 := createKeySourceJWKS(options)

		_, err := keySource2.FetchPublicKey("unknown-kid")
		assert.Equal(t, authentication.ErrKeyNotFound, err)
		assert.True(t, warnCalled)
	})

}

func createKeySourceJWKS(options *authentication.JWKSOptions) *authentication.KeySourceJWKS {
	return authentication.NewKeySourceJWKS(
		"https://example.com/.well-known/jwks.json",
		options,
	)
}
