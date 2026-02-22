package logger

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestHTTPLoggingMiddleware(t *testing.T) {
	// Create a test logger that captures output
	output := &bytes.Buffer{}
	logger := &StdLogger{
		logger: log.New(output, "", 0),
		fields: []interface{}{},
	}
	logger.level.Store(int32(model.LogLevelInfo))

	// Create a simple test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	// Wrap with logging middleware
	middleware := NewHTTPLoggingMiddleware(handler, logger)

	// Create test request
	req := httptest.NewRequest("GET", "/test?foo=bar", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()

	// Execute request
	middleware.ServeHTTP(w, req)

	// Verify log output contains expected fields
	logOutput := output.String()
	assert.Contains(t, logOutput, "INFO")
	assert.Contains(t, logOutput, "request")
	assert.Contains(t, logOutput, "remote=127.0.0.1:12345")
	assert.Contains(t, logOutput, "method=GET")
	assert.Contains(t, logOutput, "path=/test?foo=bar")
	assert.Contains(t, logOutput, "status=200")
	assert.Contains(t, logOutput, "bytes=13")
	assert.Contains(t, logOutput, "component=http")
	assert.Contains(t, logOutput, "duration=")
}

func TestHTTPLoggingMiddleware_XForwardedFor(t *testing.T) {
	output := &bytes.Buffer{}
	logger := &StdLogger{
		logger: log.New(output, "", 0),
		fields: []interface{}{},
	}
	logger.level.Store(int32(model.LogLevelInfo))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	middleware := NewHTTPLoggingMiddleware(handler, logger)

	req := httptest.NewRequest("POST", "/api/data", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	logOutput := output.String()
	// Should use X-Forwarded-For instead of RemoteAddr
	assert.Contains(t, logOutput, "remote=203.0.113.1")
	assert.Contains(t, logOutput, "status=404")
	assert.Contains(t, logOutput, "method=POST")
}

func TestHTTPLoggingMiddleware_MultipleWrites(t *testing.T) {
	output := &bytes.Buffer{}
	logger := &StdLogger{
		logger: log.New(output, "", 0),
		fields: []interface{}{},
	}
	logger.level.Store(int32(model.LogLevelInfo))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, "))
		w.Write([]byte("World!"))
	})

	middleware := NewHTTPLoggingMiddleware(handler, logger)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	logOutput := output.String()
	// Should track total bytes from multiple writes (7 + 6 = 13)
	assert.Contains(t, logOutput, "bytes=13")
}

func TestHTTPLoggingMiddleware_NoWriteHeader(t *testing.T) {
	output := &bytes.Buffer{}
	logger := &StdLogger{
		logger: log.New(output, "", 0),
		fields: []interface{}{},
	}
	logger.level.Store(int32(model.LogLevelInfo))

	// Handler that doesn't explicitly call WriteHeader
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("implicit 200"))
	})

	middleware := NewHTTPLoggingMiddleware(handler, logger)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	w := httptest.NewRecorder()

	middleware.ServeHTTP(w, req)

	logOutput := output.String()
	// Should default to 200 when WriteHeader is not called
	assert.Contains(t, logOutput, "status=200")
}
