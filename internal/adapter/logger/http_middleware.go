package logger

import (
	"net/http"
	"time"

	"github.com/goydb/goydb/pkg/port"
)

// HTTPLoggingMiddleware wraps an http.Handler to log requests in structured format
type HTTPLoggingMiddleware struct {
	handler http.Handler
	logger  port.Logger
}

// NewHTTPLoggingMiddleware creates a new HTTP logging middleware
func NewHTTPLoggingMiddleware(handler http.Handler, logger port.Logger) *HTTPLoggingMiddleware {
	return &HTTPLoggingMiddleware{
		handler: handler,
		logger:  logger.With("component", "http"),
	}
}

func (m *HTTPLoggingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Wrap ResponseWriter to capture status code and bytes written
	wrapped := &responseWriter{
		ResponseWriter: w,
		statusCode:     200, // default
	}

	// Call the actual handler
	m.handler.ServeHTTP(wrapped, r)

	// Log after request completes
	duration := time.Since(start)

	m.logger.Infof(r.Context(), "request",
		"remote", getRemoteAddr(r),
		"method", r.Method,
		"path", r.URL.RequestURI(),
		"proto", r.Proto,
		"status", wrapped.statusCode,
		"bytes", wrapped.bytesWritten,
		"duration", duration.String(),
	)
}

// responseWriter wraps http.ResponseWriter to capture status and bytes
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

func getRemoteAddr(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
