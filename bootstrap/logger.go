package bootstrap

import (
	"fmt"
	"log"
	"os"
)

const (
	logPrefix     = "[BOOTSTRAP]"
	logLevelInfo  = "INFO"
	logLevelError = "ERROR"
)

// Logger represents basic logging behavior
type Logger interface {
	Info(v ...any)
	Error(v ...any)
}

// DefaultLogger simple implementation of Logger
type DefaultLogger struct {
	logger *log.Logger
}

// NewDefaultLogger builds new default logger which prints logs to os.Stdout in format: [BOOTSTRAP] {INFO/ERROR} {message}
func NewDefaultLogger() *DefaultLogger {
	return &DefaultLogger{
		logger: log.New(os.Stdout, logPrefix, log.LstdFlags|log.LUTC),
	}
}

// Info logs info messages
func (s *DefaultLogger) Info(v ...any) {
	s.println(logLevelInfo, v...)
}

// Error logs error messages
func (s *DefaultLogger) Error(v ...any) {
	s.println(logLevelError, v...)
}

func (s *DefaultLogger) println(level string, v ...any) {
	s.logger.Println(fmt.Sprintf("%s:", level), fmt.Sprint(v...))
}

// NoopLogger represents logger which produce no output
type NoopLogger struct{}

// NewNoopLogger builds new NoopLogger
func NewNoopLogger() *NoopLogger {
	return &NoopLogger{}
}

// Info logs nothing
func (s *NoopLogger) Info(_ ...any) {}

// Error logs nothing
func (s *NoopLogger) Error(_ ...any) {}
