package envx

import (
	"os"
)

// EnvSource implements the Source interface for environment variables.
type EnvSource struct{}

// Lookup retrieves an environment variable by name.
func (EnvSource) Lookup(key string) (string, bool, error) {
	val, found := os.LookupEnv(key)
	return val, found, nil
}

// Name returns the source name.
func (EnvSource) Name() string {
	return "Environment"
}
