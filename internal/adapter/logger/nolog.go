package logger

import (
	"context"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// NoLog is a Logger that does nothing, useful for tests
type NoLog struct{}

var _ port.Logger = (*NoLog)(nil) // Compile-time verification

func (n NoLog) Debugf(ctx context.Context, msg string, keysAndValues ...interface{}) {}
func (n NoLog) Infof(ctx context.Context, msg string, keysAndValues ...interface{})  {}
func (n NoLog) Warnf(ctx context.Context, msg string, keysAndValues ...interface{})  {}
func (n NoLog) Errorf(ctx context.Context, msg string, keysAndValues ...interface{}) {}
func (n NoLog) With(keysAndValues ...interface{}) port.Logger                         { return n }
func (n NoLog) Level() model.LogLevel                                                 { return model.LogLevelError }
func (n NoLog) SetLevel(level model.LogLevel)                                         {}

// New returns a Logger that does nothing, useful for tests
func NewNoLog() port.Logger {
	return NoLog{}
}
