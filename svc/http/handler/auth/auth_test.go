package auth_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"go.uber.org/mock/gomock"

	. "github.com/velmie/x/svc/http/handler/auth"
	. "github.com/velmie/x/svc/http/handler/auth/mock"
)

func TestHandler(t *testing.T) {
	type mocks struct {
		extractor      *MockTokenExtractor
		method         *MockMethod
		successHandler *MockSuccessHandler
		errHandler     *MockErrorHandler
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
				m.errHandler.EXPECT().HandleError(gomock.Any(), gomock.Any(), gomock.Any())
			},
			expectedError: true,
		},
		{
			name: "Authentication failed",
			setupMocks: func(m *mocks) {
				m.extractor.EXPECT().Extract(gomock.Any()).Return("token", nil)
				m.method.EXPECT().Authenticate(context.Background(), "token").Return(nil, errors.New("auth error"))
				m.errHandler.EXPECT().HandleError(gomock.Any(), gomock.Any(), gomock.Any())
			},
			expectedError: true,
		},
		{
			name: "All assertions pass and HandleSuccess is called",
			setupMocks: func(m *mocks) {
				m.extractor.EXPECT().Extract(gomock.Any()).Return("token", nil)
				m.method.EXPECT().Authenticate(context.Background(), "token").Return(Entity{}, nil)
				m.successHandler.EXPECT().HandleSuccess(gomock.Any(), gomock.Any(), gomock.Any())
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			m := &mocks{
				extractor:      NewMockTokenExtractor(ctrl),
				method:         NewMockMethod(ctrl),
				successHandler: NewMockSuccessHandler(ctrl),
				errHandler:     NewMockErrorHandler(ctrl),
			}

			tt.setupMocks(m)

			h := Handler(m.extractor, m.method, m.successHandler, m.errHandler)
			req := httptest.NewRequest("GET", "http://example.com", nil)
			w := httptest.NewRecorder()
			h(w, req)
		})
	}
}
