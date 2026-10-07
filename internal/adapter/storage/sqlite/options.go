//go:build sqlite

package sqlite

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/goydb/goydb/pkg/port"
)

// poolConfig holds every tunable connection-pool setting, defaulted to
// today's hardcoded values — Open(path) with no options is exactly
// equivalent to the original, non-configurable behavior.
type poolConfig struct {
	maxOpenReaders int // 0 = unbounded; see Option doc and the
	// deadlock-risk warning in OptionsFromConfig below.
	maxIdleReaders    int
	readerIdleTimeout time.Duration // 0 = never close idle reader connections
	writerIdleTimeout time.Duration // 0 = never close the idle writer connection
}

func defaultPoolConfig() poolConfig {
	return poolConfig{
		maxOpenReaders:    0,
		maxIdleReaders:    5,
		readerIdleTimeout: 5 * time.Minute,
		writerIdleTimeout: 5 * time.Minute,
	}
}

// Option configures DB's connection pools. See DB's doc comment (db.go) for
// why writePool's open/idle connection *count* is not configurable — only
// its idle timeout is — while readPool's are.
type Option func(*poolConfig)

// WithMaxOpenReaders caps readPool's open connections. 0 (the default)
// means unbounded.
//
// WARNING: a positive value reintroduces a real deadlock risk. readPool
// serves not just plain reads but the "view phase" of every WriteTransaction
// too, and that nests routinely (CreateDatabase -> BuildIndices ->
// Iterator/AllDocs -> another WriteTransaction, each holding its own
// readPool connection for the duration — see db.go's WriteTransaction doc
// comment). A cap set below the real concurrent-nesting depth can leave
// every connection held by an outer frame waiting on an inner one that can
// never acquire a connection — a true circular wait, not just contention.
// Leave at 0 unless you have a specific, well-understood reason not to.
func WithMaxOpenReaders(n int) Option {
	return func(c *poolConfig) { c.maxOpenReaders = n }
}

// WithMaxIdleReaders sets readPool's steady-state warm-connection count.
// Always safe — only affects idle connections, never blocks an acquisition.
func WithMaxIdleReaders(n int) Option {
	return func(c *poolConfig) { c.maxIdleReaders = n }
}

// WithReaderIdleTimeout sets how long an idle readPool connection is kept
// before being closed. 0 means never (matches database/sql's own
// SetConnMaxIdleTime semantics for d <= 0).
func WithReaderIdleTimeout(d time.Duration) Option {
	return func(c *poolConfig) { c.readerIdleTimeout = d }
}

// WithWriterIdleTimeout sets how long the idle writePool connection is kept
// before being closed. 0 means never.
func WithWriterIdleTimeout(d time.Duration) Option {
	return func(c *poolConfig) { c.writerIdleTimeout = d }
}

// OptionsFromConfig reads the "sqlite" config section via configGet
// (matching a *handler.ConfigStore's Get method's signature — a plain
// function value, not the concrete type, to avoid an import cycle between
// this package's callers and internal/handler; see
// internal/adapter/storage/engine_sqlite.go) and returns the equivalent
// Options. A malformed value falls back to the default and logs a warning
// rather than failing the database open over a config typo, consistent
// with how other _config values are read leniently elsewhere in goydb.
//
// warnOnce dedupes the max_open_readers risk warning (below) across
// repeated calls — NewSQLiteEngineFactory calls this once per database
// opened, and with potentially many databases (see the "many databases"
// resource-scaling concern elsewhere in this plan) that single warning
// shouldn't repeat once per database. Callers own the *sync.Once (rather
// than this package holding a package-level one) deliberately: a hidden
// process-global would make this function's behavior depend on call order
// across unrelated callers — exactly the kind of thing that makes tests
// (and multi-factory embedding scenarios) order-dependent and flaky.
func OptionsFromConfig(configGet func(section, key string) (string, bool), logger port.Logger, warnOnce *sync.Once) []Option {
	ctx := context.Background()
	var opts []Option

	if v, ok := configGet("sqlite", "max_open_readers"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			opts = append(opts, WithMaxOpenReaders(n))
			if n > 0 {
				warnOnce.Do(func() {
					logger.Warnf(ctx, "sqlite: max_open_readers is set to a positive value — "+
						"this can deadlock under concurrent nested transactions (e.g. building "+
						"several views at once) if set below the real concurrent-nesting depth; "+
						"leave at 0 (unbounded) unless you have a specific reason not to",
						"max_open_readers", n)
				})
			}
		} else {
			logger.Warnf(ctx, "sqlite: invalid max_open_readers, using default", "value", v, "error", err)
		}
	}

	if v, ok := configGet("sqlite", "max_idle_readers"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			opts = append(opts, WithMaxIdleReaders(n))
		} else {
			logger.Warnf(ctx, "sqlite: invalid max_idle_readers, using default", "value", v, "error", err)
		}
	}

	if v, ok := configGet("sqlite", "reader_idle_timeout"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			opts = append(opts, WithReaderIdleTimeout(d))
		} else {
			logger.Warnf(ctx, "sqlite: invalid reader_idle_timeout, using default", "value", v, "error", err)
		}
	}

	if v, ok := configGet("sqlite", "writer_idle_timeout"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			opts = append(opts, WithWriterIdleTimeout(d))
		} else {
			logger.Warnf(ctx, "sqlite: invalid writer_idle_timeout, using default", "value", v, "error", err)
		}
	}

	return opts
}
