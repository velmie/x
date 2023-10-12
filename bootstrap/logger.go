package bootstrap

// Logger represents basic logging behavior
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
