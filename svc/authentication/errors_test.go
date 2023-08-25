package authentication_test

import (
	"context"
	"errors"
	"testing"

	"github.com/velmie/x/authentication"

	. "github.com/velmie/x/svc/authentication"
)

// MockMethod represents a mock authentication method
type MockMethod struct {
	shouldErr bool
	err       error
}

func (m *MockMethod) Authenticate(ctx context.Context, token string) (authentication.Entity, error) {
	if m.shouldErr {
		return nil, m.err
	}
	return authentication.Entity{}, nil
}

func TestErrorAdapter(t *testing.T) {
	unknownErr := errors.New("unknown error")
	tests := []struct {
		name      string
		mockErr   error
		expectErr error
		noErr     bool
	}{
		{
			name:      "handles bad token error",
			mockErr:   authentication.ErrBadToken,
			expectErr: ErrBadToken,
		},
		{
			name:      "handles not authenticated error",
			mockErr:   authentication.ErrNotAuthenticated,
			expectErr: ErrNotAuthenticated,
		},
		{
			name:      "handles unknown error",
			mockErr:   unknownErr,
			expectErr: unknownErr,
		},
		{
			name:  "no error",
			noErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMethod := &MockMethod{
				shouldErr: true,
				err:       tt.mockErr,
			}
			adapter := NewErrorAdapter(mockMethod)

			_, err := adapter.Authenticate(context.Background(), "testToken")

			if tt.noErr {
				if err != nil {
					t.Errorf("unexpected error: %s", err)
				}
				return
			}

			if !errors.Is(err, tt.expectErr) {
				t.Errorf("expected error: %v, got: %v", tt.expectErr, err)
			}
		})
	}
}
