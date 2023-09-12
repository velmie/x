package requestauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/velmie/x/svc/http/requestauth"
	. "github.com/velmie/x/svc/http/requestauth/mock"

	"go.uber.org/mock/gomock"
)

func TestHandler(t *testing.T) {
	type mocks struct {
		extractor *MockTokenExtractor
		method    *MockMethod
		injector  *MockInjector
	}

	type setupMocks func(m *mocks)

	tests := []struct {
		name          string
		setupMocks    setupMocks
		expectedError bool
	}{
		{
			name: "Failed to extract token",
			setupMocks: func(m *mocks) {
				m.extractor.EXPECT().Extract(gomock.Any()).Return("", errors.New("extract error"))
			},
			expectedError: true,
		},
		{
			name: "Authentication failed",
			setupMocks: func(m *mocks) {
				m.extractor.EXPECT().Extract(gomock.Any()).Return("token", nil)
				m.method.EXPECT().Authenticate(context.Background(), "token").Return(nil, errors.New("auth error"))
			},
			expectedError: true,
		},
		{
			name: "All assertions pass and InjectAuth is called",
			setupMocks: func(m *mocks) {
				entity := Entity{"test": "claim"}
				m.extractor.EXPECT().Extract(gomock.Any()).Return("token", nil)
				m.method.EXPECT().Authenticate(context.Background(), "token").Return(entity, nil)
				m.injector.EXPECT().
					InjectAuth(entity, gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ Entity, _ http.ResponseWriter, r *http.Request) (*http.Request, error) {
						return r, nil
					})
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			m := &mocks{
				extractor: NewMockTokenExtractor(ctrl),
				method:    NewMockMethod(ctrl),
				injector:  NewMockInjector(ctrl),
			}

			tt.setupMocks(m)

			h := NewPipeline(m.extractor, m.method, m.injector)
			req := httptest.NewRequest("GET", "https://example.com", http.NoBody)
			w := httptest.NewRecorder()
			h(w, req)
		})
	}
}
