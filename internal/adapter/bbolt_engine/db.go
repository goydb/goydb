package bbolt_engine

import (
	"github.com/goydb/goydb/pkg/port"
	"go.etcd.io/bbolt"
)

var _ port.DatabaseEngine = (*DB)(nil)

type DB struct {
	db *bbolt.DB
}

func Open(path string) (*DB, error) {
	db, err := bbolt.Open(path, 0666, nil)
	if err != nil {
		return nil, err
	}
	return &DB{
		db: db,
	}, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}

func (db *DB) ReadTransaction(fn func(tx port.EngineReadTransaction) error) error {
	return db.db.View(func(btx *bbolt.Tx) error {
		return fn(NewReadTransaction(btx))
	})
}

// WriteTransaction executes the given function in a read transaction
// that collects all database updates into an operation log that will
// be executed at the end of the transaction execution as one transaction.
// This method is designed to allow more concurrent write transactions due
// to less time spend waiting between the different write operations.
// If no writes are made, the update transaction is omitted.
func (db *DB) WriteTransaction(fn func(tx port.EngineWriteTransaction) error) error {
	var wtx *WriteTransaction
	err := db.db.View(func(btx *bbolt.Tx) error {
		wtx = NewWriteTransaction(btx)
		return fn(wtx)
	})
	if err != nil {
		return err
	}

	// only attempt the update transaction if there is something to do
	if len(wtx.opLog) > 0 {
		return db.db.Update(func(btx *bbolt.Tx) error {
			return wtx.Commit(btx)
		})
	}

	return nil
}
