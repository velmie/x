package envx

// MapSource implements Source for a map[string]string.
// Useful for testing or in-memory configuration.
type MapSource struct {
	// Name identifies this source for logging/debugging
	SourceName string
	// Data holds the key-value pairs
	Data map[string]string
}

// NewMapSource creates a new MapSource with an optional name.
func NewMapSource(data map[string]string, name string) *MapSource {
	if name == "" {
		name = "Map"
	}
	return &MapSource{
		SourceName: name,
		Data:       data,
	}
}

// Lookup retrieves a value from the map.
func (s *MapSource) Lookup(key string) (string, bool, error) {
	val, found := s.Data[key]
	return val, found, nil
}

// Name returns the source name for logging purposes.
func (s *MapSource) Name() string {
	return s.SourceName
}
