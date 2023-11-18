package ipx

import (
	"net/http"
	"testing"
)

func TestHTTPRequestReader(t *testing.T) {
	tests := []struct {
		name          string
		headerKey     string
		headerValue   string
		remoteAddr    string
		expectedValue string
	}{
		{
			name:          "GetHeader",
			headerKey:     "X-Real-IP",
			headerValue:   "1.2.3.4",
			remoteAddr:    "",
			expectedValue: "1.2.3.4",
		},
		{
			name:          "GetRemoteAddr",
			headerKey:     "",
			headerValue:   "",
			remoteAddr:    "1.2.3.4",
			expectedValue: "1.2.3.4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				RemoteAddr: tt.remoteAddr,
				Header:     http.Header{},
			}
			req.Header.Set(tt.headerKey, tt.headerValue)
			reader := NewHTTPRequestReader(req)
			switch tt.name {
			case "GetHeader":
				if got := reader.GetHeader(tt.headerKey); got != tt.expectedValue {
					t.Errorf("Expected %s, but got %s", tt.expectedValue, got)
				}
			case "GetRemoteAddr":
				if got := reader.GetRemoteAddr(); got != tt.expectedValue {
					t.Errorf("Expected %s, but got %s", tt.expectedValue, got)
				}
			}
		})
	}
}
