package envx

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvFileSource implements Source interface to load environment variables from .env files.
type EnvFileSource struct {
	filePath string
	values   map[string]string
}

// NewEnvFileSource creates a new EnvFileSource that loads variables from the specified .env file.
// It immediately reads and parses the file during initialization.
func NewEnvFileSource(filePath string) (*EnvFileSource, error) {
	source := &EnvFileSource{
		filePath: filePath,
		values:   make(map[string]string),
	}

	if err := source.load(); err != nil {
		return nil, fmt.Errorf("failed to load env file: %w", err)
	}

	return source, nil
}

// Lookup retrieves a value by name from the loaded .env file.
func (s *EnvFileSource) Lookup(name string) (string, bool, error) {
	value, found := s.values[name]
	return value, found, nil
}

// Name returns the name of this source including the file path.
func (s *EnvFileSource) Name() string {
	return fmt.Sprintf("env-file[%s]", s.filePath)
}

// Reload reloads the values from the env file.
func (s *EnvFileSource) Reload() error {
	return s.load()
}

// load reads and parses the .env file.
func (s *EnvFileSource) load() error {
	file, err := os.Open(s.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Clear existing values
	s.values = make(map[string]string)

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse the line as KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format at line %d: expected KEY=VALUE", lineNum)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Handle quoted values
		if len(value) > 1 {
			if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
				(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
				value = value[1 : len(value)-1]
			}
		}

		// Validate key
		if key == "" {
			return errors.New("empty key found")
		}

		s.values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error scanning file: %w", err)
	}

	return nil
}
