//go:build sqlite

package sqlite

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapterlogger "github.com/goydb/goydb/internal/adapter/logger"
	"github.com/goydb/goydb/pkg/port"
)

// recordingLogger wraps a no-op Logger, capturing Warnf calls so tests can
// assert on warning behavior (e.g. "exactly once") without a real logger.
type recordingLogger struct {
	port.Logger
	mu    sync.Mutex
	warns []string
}

func newRecordingLogger() *recordingLogger {
	return &recordingLogger{Logger: adapterlogger.NewNoLog()}
}

func (r *recordingLogger) Warnf(ctx context.Context, msg string, keysAndValues ...interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warns = append(r.warns, msg)
}

func (r *recordingLogger) warnCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.warns)
}

// --- individual options actually change pool behavior ----------------------

func TestOption_MaxOpenReaders_ActuallyLimitsConcurrency(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test"), WithMaxOpenReaders(2))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const n = 2
	release := make(chan struct{})
	started := make(chan struct{}, n)
	done := make(chan struct{}, n)

	for i := 0; i < n; i++ {
		go func() {
			_ = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
				started <- struct{}{}
				<-release // hold the connection open until told to proceed
				return nil
			})
			done <- struct{}{}
		}()
	}

	// Both of the first two should start concurrently — the cap is 2.
	for i := 0; i < n; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("expected both readers (within the cap) to start")
		}
	}

	// A third reader must NOT be able to start while the first two still
	// hold their connections — proves the cap is actually enforced, not
	// just accepted and ignored.
	thirdStarted := make(chan struct{}, 1)
	go func() {
		_ = db.ReadTransaction(func(tx port.EngineReadTransaction) error {
			thirdStarted <- struct{}{}
			return nil
		})
	}()

	select {
	case <-thirdStarted:
		t.Fatal("third reader started before a connection was released — cap not enforced")
	case <-time.After(300 * time.Millisecond):
		// expected: still blocked
	}

	close(release)
	for i := 0; i < n; i++ {
		<-done
	}

	select {
	case <-thirdStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("third reader never started after connections were released")
	}
}

func TestOption_ReaderIdleTimeoutZero_NeverClosesIdleConnections(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test"), WithReaderIdleTimeout(0))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// A handful of reads to actually open and then idle a connection.
	for i := 0; i < 5; i++ {
		err := db.ReadTransaction(func(tx port.EngineReadTransaction) error {
			_, _ = tx.Get([]byte("meta"), []byte("whatever"))
			return nil
		})
		require.NoError(t, err)
	}

	stats := db.readPool.Stats()
	assert.Zero(t, stats.MaxIdleTimeClosed, "no connection should ever be closed for idling when ReaderIdleTimeout is 0")
}

// --- OptionsFromConfig -------------------------------------------------

func fakeConfigGet(values map[string]string) func(section, key string) (string, bool) {
	return func(section, key string) (string, bool) {
		if section != "sqlite" {
			return "", false
		}
		v, ok := values[key]
		return v, ok
	}
}

func TestOptionsFromConfig_ValidValuesApplied(t *testing.T) {
	logger := newRecordingLogger()
	get := fakeConfigGet(map[string]string{
		"max_open_readers":    "7",
		"max_idle_readers":    "3",
		"reader_idle_timeout": "90s",
		"writer_idle_timeout": "0",
	})

	opts := OptionsFromConfig(get, logger, &sync.Once{})

	var cfg poolConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	assert.Equal(t, 7, cfg.maxOpenReaders)
	assert.Equal(t, 3, cfg.maxIdleReaders)
	assert.Equal(t, 90*time.Second, cfg.readerIdleTimeout)
	assert.Equal(t, time.Duration(0), cfg.writerIdleTimeout)
	assert.Equal(t, 1, logger.warnCount(), "a positive max_open_readers should log exactly one warning")
}

func TestOptionsFromConfig_MalformedValuesFallBackToDefault(t *testing.T) {
	logger := newRecordingLogger()
	get := fakeConfigGet(map[string]string{
		"max_open_readers":    "not-a-number",
		"max_idle_readers":    "also-not-a-number",
		"reader_idle_timeout": "not-a-duration",
		"writer_idle_timeout": "not-a-duration-either",
	})

	opts := OptionsFromConfig(get, logger, &sync.Once{})
	assert.Empty(t, opts, "every value is malformed, so no options should be derived — defaults apply")
	assert.Equal(t, 4, logger.warnCount(), "one warning per malformed value")
}

func TestOptionsFromConfig_MaxOpenReadersWarningLogsOnlyOnce(t *testing.T) {
	logger := newRecordingLogger()
	get := fakeConfigGet(map[string]string{"max_open_readers": "5"})

	// Simulate OptionsFromConfig being called once per database opened
	// against the SAME *sync.Once, as NewSQLiteEngineFactory's returned
	// closure does — one shared Once per factory, reused across every
	// database that factory opens.
	warnOnce := &sync.Once{}
	for i := 0; i < 10; i++ {
		OptionsFromConfig(get, logger, warnOnce)
	}

	assert.Equal(t, 1, logger.warnCount(), "the deadlock-risk warning must be deduped across calls, not repeated per database")
}

func TestOptionsFromConfig_ZeroMaxOpenReadersDoesNotWarn(t *testing.T) {
	logger := newRecordingLogger()
	get := fakeConfigGet(map[string]string{"max_open_readers": "0"})

	OptionsFromConfig(get, logger, &sync.Once{})

	assert.Zero(t, logger.warnCount(), "the default, safe value (0, unbounded) should never warn")
}
