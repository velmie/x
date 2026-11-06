package identity_test

import (
	"strings"
	"testing"

	"github.com/velmie/x/svc/identity"
)

func TestIdentityBounds(t *testing.T) {
	tests := []struct {
		name  string
		check func(string) bool
		limit int
	}{
		{
			name:  "issuer",
			check: identity.ValidIssuer,
			limit: identity.MaxIssuerBytes,
		},
		{
			name:  "subject",
			check: identity.ValidSubject,
			limit: identity.MaxSubjectBytes,
		},
	}
	for _, test := range tests {
		t.Run(
			test.name,
			func(t *testing.T) {
				cases := []struct {
					name  string
					value string
					want  bool
				}{
					{
						name: "empty",
					},
					{
						name:  "unicode whitespace",
						value: " \t\u2003",
					},
					{
						name:  "invalid UTF-8",
						value: string([]byte{0xff}),
					},
					{
						name:  "exact byte limit",
						value: strings.Repeat("a", test.limit),
						want:  true,
					},
					{
						name:  "above byte limit",
						value: strings.Repeat("a", test.limit+1),
					},
					{
						name:  "multibyte exact limit",
						value: strings.Repeat("é", test.limit/2),
						want:  true,
					},
					{
						name:  "surrounding whitespace preserved",
						value: "  abc  ",
						want:  true,
					},
				}
				for _, tc := range cases {
					t.Run(
						tc.name,
						func(t *testing.T) {
							if got := test.check(tc.value); got != tc.want {
								t.Fatalf("check(%q) = %v, want %v", tc.value, got, tc.want)
							}
						},
					)
				}
			},
		)
	}
}

func TestPrincipalValidityAndPermissions(t *testing.T) {
	var zero identity.Principal
	if zero.Valid() || zero.Has("read") {
		t.Fatal("zero principal must be invalid and have no permissions")
	}
	p := identity.Principal{
		Issuer:      " issuer ",
		Subject:     " subject ",
		Permissions: []string{"read", "", "write"},
	}
	if !p.Valid() || !p.Has("read") || !p.Has("") || p.Has("READ") || p.Has("*") {
		t.Fatalf("unexpected principal validity or exact permission match: %+v", p)
	}
	if p.Issuer != " issuer " || p.Subject != " subject " {
		t.Fatal("identity text changed")
	}
}
