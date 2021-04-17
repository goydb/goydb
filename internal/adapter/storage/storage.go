package storage

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"sync"
)

type Storage struct {
	path string
	dbs  map[string]*Database
	mu   sync.RWMutex
}

func Open(path string) (*Storage, error) {
	s := &Storage{
		path: path,
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

		database, err := s.CreateDatabase(ctx, path.Base(f.Name()))
		if err != nil {
			log.Printf("Loading DB %q failed: %v", f.Name(), err)
			return err
		}
		log.Printf("Loaded %s", database)
	}

	return nil
}

func (s *Storage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, db := range s.dbs {
		// TODO: check on better options
		err := db.Close()
		if err != nil {
			return fmt.Errorf("failed to close db %q: %w", name, err)
		}
	}

	return nil
}
