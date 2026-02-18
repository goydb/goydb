package storage

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/goydb/goydb/pkg/port"
)

type Storage struct {
	path           string
	dbs            map[string]*Database
	mu             sync.RWMutex
	viewEngines    port.ViewEngines
	filterEngines  port.FilterEngines
	reducerEngines port.ReducerEngines
	logger         port.Logger
}

type StorageOption func(s *Storage) error

func Open(path string, options ...StorageOption) (*Storage, error) {
	s := &Storage{
		path:           path,
		viewEngines:    make(port.ViewEngines),
		filterEngines:  make(port.FilterEngines),
		reducerEngines: make(port.ReducerEngines),
	}

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
	s.mu.Unlock()

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		// Skip non-database files (e.g. _config.json). CouchDB database names
		// never contain a dot, so any file with an extension is not a database.
		if strings.Contains(f.Name(), ".") {
			continue
		}

		s.logger.Infof(ctx, "loading database", "name", path.Base(f.Name()))
		database, err := s.CreateDatabase(ctx, path.Base(f.Name()))
		if err != nil {
			s.logger.Warnf(ctx, "database load failed", "name", f.Name(), "error", err)
			return err
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

func WithLogger(logger port.Logger) StorageOption {
	return func(s *Storage) error {
		s.logger = logger
		return nil
	}
}
