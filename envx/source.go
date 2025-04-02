package envx

// Source is an interface for any data source that can be used to lookup configuration values.
type Source interface {
	// Lookup retrieves a value by name from the source.
	// It returns the value as a string, a boolean flag indicating if the value was found,
	// and an error if there was a problem accessing the source.
	Lookup(name string) (value string, found bool, err error)

	// Name returns a human-readable name of the source for debugging or logging purposes.
	Name() string
}
