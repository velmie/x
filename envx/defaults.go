package envx

// DefaultResolver is the global resolver used by the package functions.
var DefaultResolver Resolver = initDefaultResolver()

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

func initDefaultResolver() Resolver {
	resolver := NewResolver().WithErrorHandler(ContinueOnError)
	resolver.AddSource(EnvSource{}, WithLabels("env", "default"))
	return resolver
}
