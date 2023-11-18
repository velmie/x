package ipx

import (
	"errors"
	"net"
	"testing"
)

type MockRequestReader struct {
	header string
	addr   string
}

func (m *MockRequestReader) GetHeader(_ string) string {
	return m.header
}

func (m *MockRequestReader) GetRemoteAddr() string {
	return m.addr
}

type MockCIDRParser struct {
	err error
}

func (m *MockCIDRParser) Parse(cidr string) (Range, error) {
	if m.err != nil {
		return Range{}, m.err
	}

	return Range{
		Start: net.ParseIP("192.0.2.0"),
		End:   net.ParseIP("192.0.2.255"),
	}, nil
}

func TestRealIPGetter(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies []string
		privateSubnets []string
		ipHeaders      []string
		remoteAddr     string
		headerVal      string
		expectedIP     string
	}{
		{
			name:           "Return global IP",
			trustedProxies: []string{"192.0.2.0/24"},
			privateSubnets: []string{"192.0.2.0/24"},
			ipHeaders:      []string{"X-Real-IP"},
			remoteAddr:     "192.0.2.1",
			headerVal:      "203.0.115.1",
			expectedIP:     "203.0.115.1",
		},
		{
			name:           "Return ip avoiding private subnets",
			trustedProxies: []string{"192.0.2.0/24"},
			privateSubnets: []string{"192.0.2.0/24"},
			ipHeaders:      []string{"X-Real-IP"},
			remoteAddr:     "192.0.2.1",
			headerVal:      "192.0.2.5, 192.0.2.55, 8.8.8.8",
			expectedIP:     "8.8.8.8",
		},
		{
			name:       "Return remote addr",
			ipHeaders:  []string{"X-Real-IP"},
			remoteAddr: "8.8.8.8",
			headerVal:  "192.0.2.1",
			expectedIP: "8.8.8.8",
		},
		{
			name:           "Return remoteAddr if header value is private",
			trustedProxies: []string{"192.0.2.0/24"},
			privateSubnets: []string{"192.0.2.0/24"},
			ipHeaders:      []string{"X-Real-IP"},
			remoteAddr:     "1.2.3.4",
			headerVal:      "192.0.2.5",
			expectedIP:     "1.2.3.4",
		},
	}

	parser := &MockCIDRParser{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getter := NewRealIPGetter(parser)
			err := getter.AddTrustedProxies(tt.trustedProxies...)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			err = getter.AddPrivateSubnets(tt.privateSubnets...)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			getter.AddIPHeaders(tt.ipHeaders...)

			requestReader := &MockRequestReader{
				header: tt.headerVal,
				addr:   tt.remoteAddr,
			}

			ip := getter.GetRealIP(requestReader)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %s, but got %s", tt.expectedIP, ip)
			}
		})
	}
}

func TestAddTrustedProxiesError(t *testing.T) {
	getter := NewRealIPGetter(&MockCIDRParser{err: errors.New("mock error")})
	err := getter.AddTrustedProxies("192.0.2.0/24")
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	}
}

func TestAddPrivateSubnetsError(t *testing.T) {
	getter := NewRealIPGetter(&MockCIDRParser{err: errors.New("mock error")})
	err := getter.AddPrivateSubnets("192.0.2.0/24")
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	}
}
