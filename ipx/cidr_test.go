package ipx

import (
	"net"
	"testing"
)

func TestDefaultCIDRParser_Parse(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		wantStart net.IP
		wantEnd   net.IP
		wantErr   bool
	}{
		{
			name:      "Valid CIDR block",
			cidr:      "192.0.2.0/24",
			wantStart: net.ParseIP("192.0.2.0"),
			wantEnd:   net.ParseIP("192.0.2.255"),
			wantErr:   false,
		},
		{
			name:      "Valid IPv6 CIDR block",
			cidr:      "2001:db8::/32",
			wantStart: net.ParseIP("2001:db8::"),
			wantEnd:   net.ParseIP("2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"),
			wantErr:   false,
		},
		{
			name:    "Invalid CIDR block",
			cidr:    "not.a.cidr",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDefaultCIDRParser()
			got, err := p.Parse(tt.cidr)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && (got.Start.String() != tt.wantStart.String() || got.End.String() != tt.wantEnd.String()) {
				t.Errorf("Parse() got = %v, want %v", got, Range{tt.wantStart, tt.wantEnd})
			}
		})
	}
}
