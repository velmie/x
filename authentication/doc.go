// Package authentication verifies credentials using explicitly supplied parsers
// and key sources. Managed JWKS sources expose startup, refresh and joined
// shutdown; callers own their HTTP clients and transport policy.
package authentication
