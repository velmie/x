package identity_test

import (
	"fmt"

	"github.com/velmie/x/svc/identity"
)

func ExamplePrincipal_Has() {
	principal := identity.Principal{
		Issuer:      "https://issuer.example",
		Subject:     "user-42",
		Permissions: []string{"books:read"},
	}
	fmt.Println(principal.Valid(), principal.Has("books:read"), principal.Has("books:write"))
	// Output: true true false
}

func ExampleValidIssuer() {
	fmt.Println(identity.ValidIssuer("https://issuer.example"), identity.ValidIssuer(" "))
	// Output: true false
}

func ExampleValidSubject() {
	fmt.Println(identity.ValidSubject("user-42"), identity.ValidSubject(""))
	// Output: true false
}
