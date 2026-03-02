package storage

import (
	"context"
	"fmt"
	"os"
	"path"
	"strconv"
	"sync"

	"github.com/goydb/goydb/internal/adapter/bbolt_engine"
	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type Database struct {
	name        string
	databaseDir string
	db          port.DatabaseEngine

	listener sync.Map

	indices         map[string]port.DocumentIndex
	viewEngines     map[string]port.ViewServerBuilder
	filterEngines   map[string]port.FilterServerBuilder
	reducerEngines  map[string]port.ReducerServerBuilder
	validateEngines map[string]port.ValidateServerBuilder
	logger          port.Logger
}

func (d *Database) ChangesIndex() port.DocumentIndex {
	return d.indices[index.ChangesIndexName]
}

func (d *Database) Indices() map[string]port.DocumentIndex {
	return d.indices
}

func (d *Database) Name() string {
	return d.name
}

func (d *Database) String() string {
	stats, err := d.db.Stats()
	if err == nil {
		return fmt.Sprintf("<Database name=%q stats=%+v>", d.name, stats)
	}

	return fmt.Sprintf("<Database name=%q stats=%v>", d.name, err)
}

func (d *Database) Stats(ctx context.Context) (stats model.DatabaseStats, err error) {
	return d.db.Stats()
}

func (d *Database) Compact(ctx context.Context) error {
	if err := d.compactDocuments(ctx); err != nil {
		return err
	}
	return d.db.Compact()
}

func (d *Database) Sequence(ctx context.Context) (string, error) {
	var seq uint64
	err := d.rawTx(func(tx *Transaction) error {
		seq = tx.Sequence(model.DocsBucket)
		return nil
	})
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(seq, 10), nil
}

func (d *Database) ViewEngine(name string) port.ViewServerBuilder {
	return d.viewEngines[name]
}

func (d *Database) FilterEngine(name string) port.FilterServerBuilder {
	return d.filterEngines[name]
}

func (d *Database) ReducerEngine(name string) port.ReducerServerBuilder {
	return d.reducerEngines[name]
}

func (d *Database) ValidateEngine(name string) port.ValidateServerBuilder {
	return d.validateEngines[name]
}

func (s *Storage) CreateDatabase(ctx context.Context, name string) (port.Database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	databaseDir := path.Join(s.path, name+".d")

	s.logger.Debugf(ctx, "opening database")
	db, err := bbolt_engine.Open(path.Join(s.path, name))
	if err != nil {
		return nil, err
	}
	s.logger.Debugf(ctx, "database opened")

	database := &Database{
		name:        name,
		databaseDir: databaseDir,
		db:          db,
		indices: map[string]port.DocumentIndex{
			index.ChangesIndexName: index.NewChangesIndex(),
			index.DeletedIndexName: index.NewDeletedIndex(),
		},
		viewEngines:     s.viewEngines,
		filterEngines:   s.filterEngines,
		reducerEngines:  s.reducerEngines,
		validateEngines: s.validateEngines,
		logger:         s.logger.With("database", name),
	}
	s.dbs[name] = database

	// create all required database Indices
	database.logger.Debugf(ctx, "building indices")
	err = database.rawTx(func(tx *Transaction) error {
		tx.EnsureBucket(model.DocsBucket)
		tx.EnsureBucket(model.AttRefsBucket)
		tx.EnsureBucket(model.DocLeavesBucket)
		tx.EnsureBucket(model.MetaBucket)
		tx.EnsureBucket(internalDocsBucket)

		err := database.BuildIndices(ctx, tx, false)
		if err != nil {
			return err
		}

		for _, index := range database.Indices() {
			database.logger.Debugf(ctx, "ensuring index", "index", fmt.Sprintf("%v", index))
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

	// Migrate attachment storage from per-document paths to content-addressed
	// paths.  No-op for new databases or already-migrated ones.
	if err := database.migrateAttachments(ctx); err != nil {
		return nil, err
	}

	return database, nil
}

func (s *Storage) DeleteDatabase(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	db, ok := s.dbs[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownDatabase, name)
	}

	err := db.db.Close()
	if err != nil {
		return err
	}

	err = os.Remove(path.Join(s.path, name))
	if err != nil {
		return err
	}

	if err := os.RemoveAll(db.databaseDir); err != nil && !os.IsNotExist(err) {
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
