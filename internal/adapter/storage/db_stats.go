package storage

import (
	"bytes"
	"context"
	"os"

	"github.com/goydb/goydb/internal/adapter/index"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	bolt "go.etcd.io/bbolt"
)

func (d *Database) Stats(ctx context.Context) (stats port.Stats, err error) {
	fi, err := os.Stat(d.Path())
	if err != nil {
		return stats, err
	}
	stats.FileSize = uint64(fi.Size())
	err = d.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bolt.Bucket) error {
			s := b.Stats()
			// only take the doc count from the docs bucket
			if bytes.Equal(name, model.DocsBucket) {
				stats.DocCount += uint64(s.KeyN)
			}
			// if deleted index
			if bytes.Equal(name, []byte(index.DeletedIndexName)) {
				stats.DocCount -= uint64(s.KeyN)
				stats.DocDelCount = uint64(s.KeyN)
			}

			// accumulate all numbers to have accurate database statistics
			stats.Alloc += uint64(s.BranchAlloc + s.LeafAlloc)
			stats.InUse += uint64(s.BranchInuse + s.LeafInuse)
			return nil
		})
	})
	return
}
