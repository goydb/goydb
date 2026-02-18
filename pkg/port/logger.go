package port

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
)

// Logger provides structured logging with configurable levels
type Logger interface {
	// Debugf logs a debug message with optional key-value pairs
	Debugf(ctx context.Context, msg string, keysAndValues ...interface{})
	// Infof logs an info message with optional key-value pairs
	Infof(ctx context.Context, msg string, keysAndValues ...interface{})
	// Warnf logs a warning message with optional key-value pairs
	Warnf(ctx context.Context, msg string, keysAndValues ...interface{})
	// Errorf logs an error message with optional key-value pairs
	Errorf(ctx context.Context, msg string, keysAndValues ...interface{})
	// With returns a new logger with the given key-value pairs added to all log messages
	With(keysAndValues ...interface{}) Logger
	// Level returns the current log level
	Level() model.LogLevel
	// SetLevel updates the log level (for hot-reload)
	SetLevel(level model.LogLevel)
}
