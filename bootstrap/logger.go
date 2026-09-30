package bootstrap

// Logger receives concurrent lifecycle records with original error objects.
// Implementations must redact sensitive application data before serializing
// errors, retaining useful causes and service identifiers.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

// NoopLogger represents logger which produce no output
type NoopLogger struct{}

func (n NoopLogger) Info(msg string, args ...any) {}

func (n NoopLogger) Error(msg string, args ...any) {}

// NewNoopLogger builds new NoopLogger
func NewNoopLogger() *NoopLogger {
	return &NoopLogger{}
}
