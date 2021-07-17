package storage

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"sync"

	"github.com/goydb/goydb/pkg/port"
)

type Storage struct {
	path    string
	dbs     map[string]*Database
	mu      sync.RWMutex
	engines port.ViewEngines
}

type StorageOption func(s *Storage) error

func Open(path string, options ...StorageOption) (*Storage, error) {
	s := &Storage{
		path:    path,
		engines: make(port.ViewEngines),
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

func (s *Storage) String() string {
	return "<Storage path=" + s.path + ">"
}

func (s *Storage) ReloadDatabases(ctx context.Context) error {
	files, err := ioutil.ReadDir(s.path)
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

		log.Printf("Loading database %s...", path.Base(f.Name()))
		database, err := s.CreateDatabase(ctx, path.Base(f.Name()))
		if err != nil {
			log.Printf("Loading DB %q failed: %v", f.Name(), err)
			return err
		}
		log.Printf("Loaded %s", database)
	}

	return nil
}

func (s *Storage) RegisterEngine(name string, builder port.ViewServerBuilder) error {
	if _, ok := s.engines[name]; ok {
		return fmt.Errorf("engine with name %q already registered", name)
	}
	s.engines[name] = builder
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

func WithEngine(name string, builder port.ViewServerBuilder) StorageOption {
	return func(s *Storage) error {
		return s.RegisterEngine(name, builder)
	}
}
