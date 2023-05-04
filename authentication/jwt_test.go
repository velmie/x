package authentication_test

import (
	"context"
	"crypto"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/velmie/x/authentication"
)

const ec256Pem = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIG82sOHoJAM93mMiXTpbIpmjkvf2QUmf8WS7XnrrPyd3oAoGCCqGSM49
AwEHoUQDQgAE6705KrnpI+OzekE4hmzj4CBRas8nXEkffye7VNwAHAbK3SiW/1pi
k9u6mpdKqXF9Di1/EPiyv/m6d588yOvcHA==
-----END EC PRIVATE KEY-----`

type MockKeySource struct {
	mock.Mock
}

func (m *MockKeySource) FetchPublicKey(_ context.Context, kid string) (crypto.PublicKey, error) {
	args := m.Called(kid)
	return args.Get(0).(crypto.PublicKey), args.Error(1)
}

type MockJWTParser struct {
	mock.Mock
}

func (m *MockJWTParser) Parse(_ context.Context, token string, keySource authentication.KeySource) (*authentication.JSONWebToken, error) {
	args := m.Called(token, keySource)
	return args.Get(0).(*authentication.JSONWebToken), args.Error(1)
}

func TestViaJWT_Authenticate(t *testing.T) {
	t.Run("successful authentication", func(t *testing.T) {
		const (
			kid         = "my-key-id"
			name        = "John Doe"
			sub         = "1234567890"
			iat         = float64(1682945811)
			customClaim = "custom_claim"
			fakeToken   = "test"
		)

		keySource := new(MockKeySource)
		jwtParser := new(MockJWTParser)
		viaJWT := authentication.NewViaJWT(
			jwtParser,
			keySource,
			authentication.JWTWithHook(func(ctx context.Context, token *authentication.JSONWebToken) error {
				token.Claims["custom_claim"] = customClaim
				return nil
			}),
		)

		expectedEntity := authentication.Entity{
			"sub":          sub,
			"name":         name,
			"iat":          iat,
			"custom_claim": customClaim,
		}
		jsonWebToken := &authentication.JSONWebToken{
			Raw:       fakeToken,
			Header:    map[string]interface{}{"alg": "ES256", "typ": "JWT", "kid": kid},
			Claims:    expectedEntity,
			Signature: []byte("very_secure_signature_here"),
			Valid:     true,
		}
		ctx := context.Background()

		jwtParser.On("Parse", fakeToken, keySource).Return(jsonWebToken, nil)
		entity, err := viaJWT.Authenticate(ctx, fakeToken)

		assert.NoError(t, err)
		assert.Equal(t, expectedEntity, entity)
		keySource.AssertExpectations(t)
	})

	t.Run("invalid token", func(t *testing.T) {
		keySource := new(MockKeySource)
		jwtParser := authentication.NewJWTv5Parser(&jwt.Parser{})
		viaJWT := authentication.NewViaJWT(jwtParser, keySource)
		ctx := context.Background()

		invalidToken := "invalid_jwt"

		keySource.On("FetchPublicKey", mock.Anything).Return(nil, errors.New("not found"))

		entity, err := viaJWT.Authenticate(ctx, invalidToken)

		assert.Nil(t, entity)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, authentication.ErrBadToken))
	})
}

func TestJWTv5Parser_Parse(t *testing.T) {
	t.Run("successful parse", func(t *testing.T) {
		const (
			kid = "my-key-id"

			validToken = "eyJhbGciOiJFUzI1NiIsImtpZCI6Im15LWtleS1pZCIsInR5cCI6IkpXVCJ9." +
				"eyJpYXQiOjE2ODI5NDU4MTEsIm5hbWUiOiJKb2huIERvZSIsInN1YiI6IjEyMzQ1Njc4OTAifQ." +
				"iMgUvjspZ6e7WTCFYC4DeUksp0zLZdj8bC4dxith_11x4fykkUTuK2NiLHZAF_Z_cy_16Qv352YMYrpGwPZHvw"
		)
		keySource := new(MockKeySource)
		jwtParser := authentication.NewJWTv5Parser(jwt.NewParser())
		ctx := context.Background()

		key, err := jwt.ParseECPrivateKeyFromPEM([]byte(ec256Pem))
		if err != nil {
			t.Fatal("unexpected error", err)
		}

		keySource.On("FetchPublicKey", kid).Return(key.Public(), nil)

		jsonWebToken, err := jwtParser.Parse(ctx, validToken, keySource)

		assert.NoError(t, err)
		assert.NotNil(t, jsonWebToken)
		assert.True(t, jsonWebToken.Valid)
		keySource.AssertExpectations(t)
	})

	t.Run("failed parse", func(t *testing.T) {
		keySource := new(MockKeySource)
		jwtParser := authentication.NewJWTv5Parser(&jwt.Parser{})

		invalidToken := "invalid_jwt"

		keySource.On("FetchPublicKey", mock.Anything).Return(nil, errors.New("not found"))

		ctx := context.Background()
		jsonWebToken, err := jwtParser.Parse(ctx, invalidToken, keySource)

		assert.Nil(t, jsonWebToken)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, authentication.ErrBadToken))
	})
}
