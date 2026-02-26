package logger

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// StdLogger implements port.Logger using Go's standard library
type StdLogger struct {
	level      atomic.Int32 // Atomic for lock-free reads
	logger     *log.Logger
	fields     []interface{} // From With()
	mu         sync.RWMutex  // For file handle changes
	fileHandle io.WriteCloser
}

var _ port.Logger = (*StdLogger)(nil) // Compile-time verification

// New creates a logger with the given level and output writer
func New(level model.LogLevel, output io.Writer) *StdLogger {
	l := &StdLogger{
		logger: log.New(output, "", 0), // No prefix, we format ourselves
		fields: []interface{}{},
	}
	l.level.Store(int32(level))
	return l
}

// NewFromConfig creates a logger from ConfigStore getter function
func NewFromConfig(configGetter func(string, string) (string, bool)) (*StdLogger, error) {
	// Get log level from config, default to info
	levelStr, _ := configGetter("log", "level")
	if levelStr == "" {
		levelStr = "info"
	}
	level := model.ParseLogLevel(levelStr)

	// Get log file from config, default to stdout
	var output io.Writer = os.Stdout
	var fileHandle io.WriteCloser

	if filePath, ok := configGetter("log", "file"); ok && filePath != "" {
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file %q: %w", filePath, err)
		}
		output = file
		fileHandle = file
	}

	l := &StdLogger{
		logger:     log.New(output, "", 0),
		fields:     []interface{}{},
		fileHandle: fileHandle,
	}
	l.level.Store(int32(level))

	return l, nil
}

// Level returns the current log level
func (l *StdLogger) Level() model.LogLevel {
	return model.LogLevel(l.level.Load())
}

// SetLevel updates the log level (for hot-reload)
func (l *StdLogger) SetLevel(level model.LogLevel) {
	l.level.Store(int32(level))
}

// Debugf logs a debug message with optional key-value pairs
func (l *StdLogger) Debugf(ctx context.Context, msg string, keysAndValues ...interface{}) {
	if l.Level() <= model.LogLevelDebug {
		l.log("DEBUG", msg, keysAndValues...)
	}
}

// Infof logs an info message with optional key-value pairs
func (l *StdLogger) Infof(ctx context.Context, msg string, keysAndValues ...interface{}) {
	if l.Level() <= model.LogLevelInfo {
		l.log("INFO", msg, keysAndValues...)
	}
}

// Warnf logs a warning message with optional key-value pairs
func (l *StdLogger) Warnf(ctx context.Context, msg string, keysAndValues ...interface{}) {
	if l.Level() <= model.LogLevelWarn {
		l.log("WARN", msg, keysAndValues...)
	}
}

// Errorf logs an error message with optional key-value pairs
func (l *StdLogger) Errorf(ctx context.Context, msg string, keysAndValues ...interface{}) {
	if l.Level() <= model.LogLevelError {
		l.log("ERROR", msg, keysAndValues...)
	}
}

// With returns a new logger with the given key-value pairs added to all log messages
func (l *StdLogger) With(keysAndValues ...interface{}) port.Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	newFields := make([]interface{}, len(l.fields)+len(keysAndValues))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], keysAndValues)

	nl := &StdLogger{
		logger:     l.logger,
		fields:     newFields,
		fileHandle: l.fileHandle,
	}
	nl.level.Store(l.level.Load())
	return nl
}

// log formats and writes a log message
func (l *StdLogger) log(levelStr, msg string, keysAndValues ...interface{}) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Format: 2025-01-17 10:32:15 INFO message key1=value1 key2=value2
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	// Combine fields from With() and keysAndValues
	allKVs := append(l.fields, keysAndValues...)
	kvsStr := formatKVs(allKVs)

	if kvsStr != "" {
		l.logger.Printf("%s %s %s %s", timestamp, levelStr, msg, kvsStr)
	} else {
		l.logger.Printf("%s %s %s", timestamp, levelStr, msg)
	}
}

// formatKVs converts key-value pairs to "key1=value1 key2=value2"
func formatKVs(kvs []interface{}) string {
	if len(kvs) == 0 {
		return ""
	}

	var parts []string
	for i := 0; i < len(kvs); i += 2 {
		if i+1 >= len(kvs) {
			// Odd number of elements, skip the last one
			break
		}

		key := fmt.Sprintf("%v", kvs[i])
		value := fmt.Sprintf("%v", kvs[i+1])

		// Quote values with spaces
		if strings.Contains(value, " ") {
			value = fmt.Sprintf("%q", value)
		}

		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, " ")
}

// Close closes the log file handle if one was opened
func (l *StdLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fileHandle != nil {
		return l.fileHandle.Close()
	}
	return nil
}
