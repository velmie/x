package confload

import (
	"sort"
	"strings"

	"github.com/knadh/koanf/v2"
)

// UnknownKeysError reports configuration keys that are not present in the target struct.
type UnknownKeysError struct {
	Keys    []string
	Origins map[string]ValueOrigin
}

func (e *UnknownKeysError) Error() string {
	if e == nil || len(e.Keys) == 0 {
		return "unknown configuration keys"
	}

	var b strings.Builder
	b.WriteString("unknown configuration keys: ")
	for i, key := range e.Keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(key)
		if origin, ok := e.Origins[key]; ok {
			b.WriteString(" (")
			b.WriteString(string(origin.Source))
			if origin.Identifier != "" {
				b.WriteString(" ")
				b.WriteString(origin.Identifier)
			}
			b.WriteString(")")
		}
	}

	return b.String()
}

func validateStrict(
	k *koanf.Koanf,
	origins map[string]ValueOrigin,
	allowedKeys map[string]struct{},
	wildcardPrefixes []string,
	prefix string,
) error {
	keys := k.Keys()
	unknown := make([]string, 0)
	unknownOrigins := make(map[string]ValueOrigin)
	for _, key := range keys {
		if prefix != "" && key != prefix && !strings.HasPrefix(key, prefix+".") {
			continue
		}
		if isAllowedKey(key, allowedKeys, wildcardPrefixes) {
			continue
		}
		unknown = append(unknown, key)
		if origin, ok := origins[key]; ok {
			unknownOrigins[key] = origin
		}
	}

	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return &UnknownKeysError{Keys: unknown, Origins: unknownOrigins}
}

func isAllowedKey(key string, allowedKeys map[string]struct{}, wildcardPrefixes []string) bool {
	if _, ok := allowedKeys[key]; ok {
		return true
	}
	for _, prefix := range wildcardPrefixes {
		if strings.HasPrefix(key, prefix+".") {
			return true
		}
	}

	return false
}
