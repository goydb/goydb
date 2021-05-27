package storage

import (
	"context"
	"os"

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
		return bucketStats(tx.Bucket(docsBucket), &stats)
	})
	return
}

// ViewSize returns the byte size on disk
func (d *Database) ViewSize(ctx context.Context, ddfn *model.DesignDocFn) (stats port.Stats, err error) {
	err = d.View(func(tx *bolt.Tx) error {
		return bucketStats(tx.Bucket(ddfn.Bucket()), &stats)
	})
	return
}

func bucketStats(bucket *bolt.Bucket, stats *port.Stats) error {
	if bucket == nil {
		return nil
	}

	s := bucket.Stats()
	stats.DocCount = uint64(s.KeyN)
	stats.Alloc = uint64(s.BranchAlloc + s.LeafAlloc)
	stats.InUse = uint64(s.BranchInuse + s.LeafInuse)

	return nil
}
