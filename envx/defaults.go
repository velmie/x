package envx

// DefaultResolver is the global resolver used by the package functions.
// By default, it contains only an EnvSource for backward compatibility
// and uses ContinueOnError for backward compatibility.
var DefaultResolver Resolver = NewResolver(EnvSource{}).WithErrorHandler(ContinueOnError)

// Get looks up a variable by name from the DefaultResolver.
// For backward compatibility, ignores errors from the resolver.
func Get(name string) *Variable {
	v, _ := DefaultResolver.Get(name)
	return v
}

// Coalesce tries a list of variable names and returns the first one found
// using the DefaultResolver.
// For backward compatibility, ignores errors from the resolver.
func Coalesce(names ...string) *Variable {
	v, _ := DefaultResolver.Coalesce(names...)
	return v
}
