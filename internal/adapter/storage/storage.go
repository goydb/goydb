package storage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/goydb/goydb/pkg/port"
)

// sqliteDBExt is the file extension used by the SQLite engine for its main
// database file. CouchDB database names never contain a dot, so this is
// always safe to check for regardless of which engine is compiled in.
const sqliteDBExt = ".sqlite3"

type Storage struct {
	path            string
	dbs             map[string]*Database
	brokenDBs       map[string]brokenDB // name -> why it couldn't be opened at load time
	mu              sync.RWMutex
	viewEngines     port.ViewEngines
	filterEngines   port.FilterEngines
	reducerEngines  port.ReducerEngines
	validateEngines port.ValidateEngines
	updateEngines   port.UpdateEngines
	logger          port.Logger
	engines         map[string]EngineFactory // engine name -> factory, e.g. "bbolt", "sqlite"
	defaultEngine   string                   // engine used when CreateDatabase gets no "engine" arg
}

// brokenDB records a database file that exists on disk but could not be
// opened (e.g. its engine isn't compiled into this binary, or the file is
// corrupted). fileName is the raw directory entry name (e.g. "foo.sqlite3"),
// kept so DeleteDatabase can remove it without a live engine handle.
type brokenDB struct {
	fileName string
	err      error
}

type StorageOption func(s *Storage) error

// EngineFactory opens the port.DatabaseEngine backing a single database at
// the given base path (no extension — each engine owns its own file naming).
type EngineFactory func(path string) (port.DatabaseEngine, error)

// registerEngine makes an engine selectable by name via CreateDatabase's
// "engine" arg or WithDefaultEngine. Re-registering an existing name
// replaces its factory (used by e.g. pkg/goydb/feature_sqlite.go to swap
// the plain registerOptionalEngines baseline for a config-aware factory).
func (s *Storage) registerEngine(name string, f EngineFactory) {
	if s.engines == nil {
		s.engines = make(map[string]EngineFactory)
	}
	s.engines[name] = f
}

func Open(path string, options ...StorageOption) (*Storage, error) {
	s := &Storage{
		path:            path,
		viewEngines:     make(port.ViewEngines),
		filterEngines:   make(port.FilterEngines),
		reducerEngines:  make(port.ReducerEngines),
		validateEngines: make(port.ValidateEngines),
		updateEngines:   make(port.UpdateEngines),
		defaultEngine:   "bbolt",
	}
	// bbolt is always compiled in; registerOptionalEngines (engine_sqlite.go
	// / engine_sqlite_stub.go, gated by the `sqlite` build tag) adds any
	// others available in this binary.
	registerBuiltinEngines(s)

	for _, option := range options {
		err := option(s)
		if err != nil {
			return nil, err
		}
	}

	err := s.ReloadDatabases(context.Background())
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) Path() string { return s.path }

func (s *Storage) String() string {
	return "<Storage path=" + s.path + ">"
}

func (s *Storage) ReloadDatabases(ctx context.Context) error {
	files, err := os.ReadDir(s.path)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.dbs = make(map[string]*Database)
	s.brokenDBs = make(map[string]brokenDB)
	s.mu.Unlock()

	for _, f := range files {
		if f.IsDir() {
			continue
		}

		name := f.Name()
		engine := "bbolt"
		switch {
		case strings.HasSuffix(name, sqliteDBExt):
			// SQLite database file — strip the extension to recover the name.
			name = strings.TrimSuffix(name, sqliteDBExt)
			engine = "sqlite"
		case strings.Contains(name, "."):
			// Skip non-database files (e.g. _config.json, or a SQLite engine's
			// -wal/-shm/-journal sidecar files). CouchDB database names never
			// contain a dot, so any other file with an extension is not a database.
			continue
		}

		// Reopen with the engine that actually wrote this file, not
		// s.defaultEngine — both engines can coexist in the same directory
		// (e.g. a database created with ?engine=sqlite next to bbolt ones),
		// and only the file's own shape tells us which one that is.
		s.logger.Infof(ctx, "loading database", "name", name, "engine", engine)
		database, err := s.CreateDatabase(ctx, name, map[string]string{"engine": engine})
		if err != nil {
			// Don't let one unreadable database (e.g. a .sqlite3 file left
			// behind by a binary not built with -tags sqlite) take down the
			// whole server. Record it as broken and keep loading the rest —
			// it still shows up via Databases()/_all_dbs, matching the
			// CouchDB/Fauxton behavior of reporting a database that exists
			// even when it can't currently be opened.
			s.logger.Warnf(ctx, "database unavailable, continuing startup", "name", f.Name(), "engine", engine, "error", err)
			s.mu.Lock()
			s.brokenDBs[name] = brokenDB{fileName: f.Name(), err: err}
			s.mu.Unlock()
			continue
		}
		s.logger.Infof(ctx, "database loaded", "name", database.Name())
	}

	return nil
}

func (s *Storage) RegisterViewEngine(name string, builder port.ViewServerBuilder) error {
	if _, ok := s.viewEngines[name]; ok {
		return fmt.Errorf("view engine with name %q already registered", name)
	}
	s.viewEngines[name] = builder
	return nil
}

func (s *Storage) RegisterFilterEngine(name string, builder port.FilterServerBuilder) error {
	if _, ok := s.filterEngines[name]; ok {
		return fmt.Errorf("filter engine with name %q already registered", name)
	}
	s.filterEngines[name] = builder
	return nil
}

func (s *Storage) RegisterReducerEngine(name string, builder port.ReducerServerBuilder) error {
	if _, ok := s.reducerEngines[name]; ok {
		return fmt.Errorf("reducer engine with name %q already registered", name)
	}
	s.reducerEngines[name] = builder
	return nil
}

func (s *Storage) RegisterValidateEngine(name string, builder port.ValidateServerBuilder) error {
	if _, ok := s.validateEngines[name]; ok {
		return fmt.Errorf("validate engine with name %q already registered", name)
	}
	s.validateEngines[name] = builder
	return nil
}

func (s *Storage) RegisterUpdateEngine(name string, builder port.UpdateServerBuilder) error {
	if _, ok := s.updateEngines[name]; ok {
		return fmt.Errorf("update engine with name %q already registered", name)
	}
	s.updateEngines[name] = builder
	return nil
}

func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, db := range s.dbs {
		// TODO: check on better options
		err := db.db.Close()
		if err != nil {
			return fmt.Errorf("failed to close db %q: %w", name, err)
		}
	}

	return nil
}

func WithViewEngine(name string, builder port.ViewServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterViewEngine(name, builder)
	}
}

func WithFilterEngine(name string, builder port.FilterServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterFilterEngine(name, builder)
	}
}

func WithReducerEngine(name string, builder port.ReducerServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterReducerEngine(name, builder)
	}
}

func WithValidateEngine(name string, builder port.ValidateServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterValidateEngine(name, builder)
	}
}

func WithUpdateEngine(name string, builder port.UpdateServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterUpdateEngine(name, builder)
	}
}

func WithLogger(logger port.Logger) StorageOption {
	return func(s *Storage) error {
		s.logger = logger
		return nil
	}
}

// WithEngine registers (or replaces) the factory used for databases created
// or reopened with "engine": name. Called with the builtin names ("bbolt",
// "sqlite") this overrides that engine's plain, zero-option baseline — e.g.
// pkg/goydb/feature_sqlite.go uses it to swap in a config-aware SQLite
// factory once a *handler.ConfigStore exists. Called with any other name,
// it adds a new selectable engine.
func WithEngine(name string, f EngineFactory) StorageOption {
	return func(s *Storage) error {
		s.registerEngine(name, f)
		return nil
	}
}

// WithDefaultEngine sets which registered engine CreateDatabase uses when
// its args carry no "engine" key. Defaults to "bbolt".
func WithDefaultEngine(name string) StorageOption {
	return func(s *Storage) error {
		s.defaultEngine = name
		return nil
	}
}
