package confload

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidOverride identifies an override that is not a nonempty path=value pair.
var ErrInvalidOverride = errors.New("configuration override must be path=value")

// ParseOverrides validates declared paths and parses last-value-wins overrides.
// Each entry must contain a declared, nonempty path, then '=' and a value.
// Values may be empty or contain '=' and remain strings for normal decoding.
// Syntax errors wrap ErrInvalidOverride with the one-based entry position;
// undeclared paths return an UnknownKeysError. Errors do not contain values.
func ParseOverrides(fields FieldCollection, entries []string) (map[string]any, error) {
	values := make(map[string]any, len(entries))
	for i, entry := range entries {
		path, value, found := strings.Cut(entry, "=")
		if !found || path == "" {
			return nil, fmt.Errorf("override %d: %w", i+1, ErrInvalidOverride)
		}
		if _, ok := fields.Lookup(path); !ok {
			return nil, &UnknownKeysError{
				Keys: []string{path},
			}
		}
		values[path] = value
	}
	return values, nil
}
