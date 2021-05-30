package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"sync"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	bolt "go.etcd.io/bbolt"
)

type Database struct {
	name        string
	databaseDir string
	*bolt.DB

	mu       sync.RWMutex
	channels []chan *model.Document

	indices map[string]port.DocumentIndex

	searchIndices   map[string]port.SearchIndex
	muSearchIndices sync.RWMutex
}

func (d Database) ChangesIndex() port.DocumentIndex {
	return d.indices[ChangesIndexName]
}

func (d Database) Indices() map[string]port.DocumentIndex {
	return d.indices
}

func (d Database) Name() string {
	return d.name
}

func (d Database) String() string {
	stats, err := d.Stats(context.Background())
	if err == nil {
		return fmt.Sprintf("<Database name=%q stats=%+v>", d.name, stats)
	}

	return fmt.Sprintf("<Database name=%q stats=%v>", d.name, err)
}

func (d Database) Sequence() string {
	var seq uint64
	err := d.RTransaction(context.Background(), func(tx port.Transaction) error {
		seq = tx.Sequence()
		return nil
	})
	if err != nil {
		log.Fatal(err) // FIXME
	}
	return strconv.FormatUint(seq, 10)
}

func (s *Storage) CreateDatabase(ctx context.Context, name string) (port.Database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	databaseDir := path.Join(s.path, name+".d")

	db, err := bolt.Open(path.Join(s.path, name), 0666, nil)
	if err != nil {
		return nil, err
	}

	database := &Database{
		name:        name,
		databaseDir: databaseDir,
		DB:          db,
		indices: map[string]port.DocumentIndex{
			ChangesIndexName: NewChangesIndex(ChangesIndexName),
		},
		searchIndices: make(map[string]port.SearchIndex),
	}
	s.dbs[name] = database

	// create all required database Indices
	err = database.Transaction(ctx, func(tx port.Transaction) error {
		// load all design documents
		docs, _, err := database.AllDesignDocs(ctx)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			vfs := doc.ViewFunctions()
			for _, vf := range vfs {
				ddfn := model.DesignDocFn{
					Type:        model.ViewFn,
					DesignDocID: doc.ID,
					FnName:      vf.Name,
				}
				log.Printf("CREATING INDEX %s", ddfn)
			}
		}

		for _, index := range database.Indices() {
			err := index.Ensure(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// open all search Indices
	err = database.openAllSearchIndices()
	if err != nil {
		return nil, err
	}

	return database, nil
}

func (s *Storage) DeleteDatabase(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	db, ok := s.dbs[name]
	if !ok {
		return fmt.Errorf("unknown database %q", name)
	}

	err := db.Close()
	if err != nil {
		return err
	}

	err = os.Remove(path.Join(s.path, name))
	if err != nil {
		return err
	}

	delete(s.dbs, name)

	return nil
}

func (s *Storage) Databases(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, len(s.dbs))
	var i int
	for name := range s.dbs {
		names[i] = name
		i++
	}

	return names, nil
}

func (s *Storage) Database(ctx context.Context, name string) (port.Database, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	db, ok := s.dbs[name]
	if !ok {
		return nil, fmt.Errorf("database %q not found", name)
	}

	return db, nil
}
