package logger

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

func TestStdLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name       string
		logLevel   model.LogLevel
		logFunc    func(port.Logger, context.Context)
		wantOutput bool
	}{
		{
			name:     "debug logs at debug level",
			logLevel: model.LogLevelDebug,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Debugf(ctx, "test message")
			},
			wantOutput: true,
		},
		{
			name:     "debug logs filtered at info level",
			logLevel: model.LogLevelInfo,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Debugf(ctx, "test message")
			},
			wantOutput: false,
		},
		{
			name:     "info logs at info level",
			logLevel: model.LogLevelInfo,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message")
			},
			wantOutput: true,
		},
		{
			name:     "warn logs at warn level",
			logLevel: model.LogLevelWarn,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Warnf(ctx, "test message")
			},
			wantOutput: true,
		},
		{
			name:     "info logs filtered at warn level",
			logLevel: model.LogLevelWarn,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message")
			},
			wantOutput: false,
		},
		{
			name:     "error logs at error level",
			logLevel: model.LogLevelError,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Errorf(ctx, "test message")
			},
			wantOutput: true,
		},
		{
			name:     "warn logs filtered at error level",
			logLevel: model.LogLevelError,
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Warnf(ctx, "test message")
			},
			wantOutput: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := New(tt.logLevel, buf)

			tt.logFunc(logger, context.Background())

			output := buf.String()
			if tt.wantOutput && output == "" {
				t.Errorf("expected output, got none")
			}
			if !tt.wantOutput && output != "" {
				t.Errorf("expected no output, got: %s", output)
			}
		})
	}
}

func TestStdLogger_StructuredLogging(t *testing.T) {
	tests := []struct {
		name       string
		logFunc    func(port.Logger, context.Context)
		wantInLog  []string
	}{
		{
			name: "simple key-value",
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message", "key", "value")
			},
			wantInLog: []string{"INFO", "test message", "key=value"},
		},
		{
			name: "multiple key-values",
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message", "key1", "value1", "key2", "value2")
			},
			wantInLog: []string{"INFO", "test message", "key1=value1", "key2=value2"},
		},
		{
			name: "value with spaces gets quoted",
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message", "key", "value with spaces")
			},
			wantInLog: []string{"INFO", "test message", `key="value with spaces"`},
		},
		{
			name: "integer value",
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Infof(ctx, "test message", "count", 42)
			},
			wantInLog: []string{"INFO", "test message", "count=42"},
		},
		{
			name: "error value",
			logFunc: func(l port.Logger, ctx context.Context) {
				l.Errorf(ctx, "operation failed", "error", "something went wrong")
			},
			wantInLog: []string{"ERROR", "operation failed", `error="something went wrong"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := New(model.LogLevelDebug, buf)

			tt.logFunc(logger, context.Background())

			output := buf.String()
			for _, want := range tt.wantInLog {
				if !strings.Contains(output, want) {
					t.Errorf("expected %q in output, got: %s", want, output)
				}
			}
		})
	}
}

func TestStdLogger_With(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := New(model.LogLevelInfo, buf)

	// Create child logger with component field
	componentLogger := logger.With("component", "replication")
	componentLogger.Infof(context.Background(), "test message", "action", "sync")

	output := buf.String()
	wantStrings := []string{
		"INFO",
		"test message",
		"component=replication",
		"action=sync",
	}

	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got: %s", want, output)
		}
	}
}

func TestStdLogger_WithChaining(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := New(model.LogLevelInfo, buf)

	// Chain With calls
	childLogger := logger.With("component", "replication").With("repID", "abc123")
	childLogger.Infof(context.Background(), "test message")

	output := buf.String()
	wantStrings := []string{
		"component=replication",
		"repID=abc123",
	}

	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got: %s", want, output)
		}
	}
}

func TestStdLogger_SetLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := New(model.LogLevelInfo, buf)

	// Debug should be filtered at Info level
	logger.Debugf(context.Background(), "debug message")
	if buf.String() != "" {
		t.Errorf("expected no output at Info level for debug message")
	}

	// Change to Debug level
	logger.SetLevel(model.LogLevelDebug)

	// Debug should now appear
	logger.Debugf(context.Background(), "debug message")
	if !strings.Contains(buf.String(), "DEBUG") {
		t.Errorf("expected DEBUG in output after SetLevel, got: %s", buf.String())
	}

	// Change to Error level
	buf.Reset()
	logger.SetLevel(model.LogLevelError)

	// Info should be filtered
	logger.Infof(context.Background(), "info message")
	if buf.String() != "" {
		t.Errorf("expected no output at Error level for info message")
	}

	// Error should appear
	logger.Errorf(context.Background(), "error message")
	if !strings.Contains(buf.String(), "ERROR") {
		t.Errorf("expected ERROR in output, got: %s", buf.String())
	}
}

func TestStdLogger_NewFromConfig(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]map[string]string
		wantLevel model.LogLevel
		wantErr   bool
	}{
		{
			name: "debug level from config",
			config: map[string]map[string]string{
				"log": {"level": "debug"},
			},
			wantLevel: model.LogLevelDebug,
		},
		{
			name: "info level from config",
			config: map[string]map[string]string{
				"log": {"level": "info"},
			},
			wantLevel: model.LogLevelInfo,
		},
		{
			name: "warn level from config",
			config: map[string]map[string]string{
				"log": {"level": "warn"},
			},
			wantLevel: model.LogLevelWarn,
		},
		{
			name: "error level from config",
			config: map[string]map[string]string{
				"log": {"level": "error"},
			},
			wantLevel: model.LogLevelError,
		},
		{
			name:      "default to info when no config",
			config:    map[string]map[string]string{},
			wantLevel: model.LogLevelInfo,
		},
		{
			name: "default to info for invalid level",
			config: map[string]map[string]string{
				"log": {"level": "invalid"},
			},
			wantLevel: model.LogLevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getter := func(section, key string) (string, bool) {
				if sec, ok := tt.config[section]; ok {
					if val, ok := sec[key]; ok {
						return val, true
					}
				}
				return "", false
			}

			logger, err := NewFromConfig(getter)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewFromConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil && logger.Level() != tt.wantLevel {
				t.Errorf("NewFromConfig() level = %v, want %v", logger.Level(), tt.wantLevel)
			}
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  model.LogLevel
	}{
		{"debug", model.LogLevelDebug},
		{"info", model.LogLevelInfo},
		{"warn", model.LogLevelWarn},
		{"warning", model.LogLevelWarn},
		{"error", model.LogLevelError},
		{"invalid", model.LogLevelInfo}, // defaults to info
		{"", model.LogLevelInfo},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := model.ParseLogLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level model.LogLevel
		want  string
	}{
		{model.LogLevelDebug, "debug"},
		{model.LogLevelInfo, "info"},
		{model.LogLevelWarn, "warn"},
		{model.LogLevelError, "error"},
		{model.LogLevel(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.level.String()
			if got != tt.want {
				t.Errorf("LogLevel.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNoLog(t *testing.T) {
	logger := NewNoLog()

	// Should not panic
	ctx := context.Background()
	logger.Debugf(ctx, "test")
	logger.Infof(ctx, "test")
	logger.Warnf(ctx, "test")
	logger.Errorf(ctx, "test")

	child := logger.With("key", "value")
	child.Infof(ctx, "test")

	logger.SetLevel(model.LogLevelDebug)
}
