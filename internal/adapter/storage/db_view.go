package storage

import (
	"context"
	"fmt"
	"strconv"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	bolt "go.etcd.io/bbolt"
	"gopkg.in/mgo.v2/bson"
)

func viewBucket(name string) []byte {
	return []byte("view:" + name)
}

var indexBucket = viewBucket("_index")

func (d *Database) ResetView(ctx context.Context, name string) error {
	err := d.Update(func(tx *bolt.Tx) error {
		if tx.Bucket(viewBucket(name)) != nil {
			return tx.DeleteBucket(viewBucket(name))
		}
		return nil
	})
	return err
}

func (d *Database) UpdateView(ctx context.Context, name string, docs []*model.Document) error {
	err := d.Update(func(tx *bolt.Tx) error {
		bucketName := viewBucket(name)
		bucket, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}

		viewIndexBucket, err := tx.CreateBucketIfNotExists(indexBucket)
		if err != nil {
			return err
		}

		for _, doc := range docs {
			data, err := bson.Marshal(doc)
			if err != nil {
				return err
			}
			key, err := cbor.Marshal(doc.Key)
			if err != nil {
				return err
			}
			seq, err := bucket.NextSequence()
			if err != nil {
				return err
			}
			key = []byte(string(key) + " " + strconv.FormatInt(int64(seq), 10))
			//log.Println(string(key), string(data))
			err = bucket.Put(key, data)
			if err != nil {
				return err
			}
			err = addDocKeyToView(viewIndexBucket, doc, bucketName, key)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

type ViewKey struct {
	V []byte // name of the view
	K []byte // name of the key
}

func (vk ViewKey) String() string {
	return fmt.Sprintf("<ViewKey View=%q Key=%q>", vk.V, vk.K)
}

// addDocKeyToView adds to the current document ID a view key,
func addDocKeyToView(index *bolt.Bucket, doc *model.Document, bucketName, key []byte) error {
	var keys []*ViewKey
	val := index.Get([]byte(doc.ID))

	newKey := &ViewKey{
		V: bucketName,
		K: key,
	}
	if len(val) > 0 {
		err := cbor.Unmarshal(val, &keys)
		if err != nil {
			return err
		}
	}
	keys = append(keys, newKey)

	newVal, err := cbor.Marshal(keys)
	if err != nil {
		return err
	}

	return index.Put([]byte(doc.ID), newVal)
}

func (d *Database) ResetViewIndex() error {
	err := d.Update(func(tx *bolt.Tx) error {
		return tx.DeleteBucket(indexBucket)
	})
	return err
}

// ResetViewIndexForDoc remove all index values based on the doc
func (d *Database) ResetViewIndexForDoc(ctx context.Context, docID string) error {
	err := d.Update(func(tx *bolt.Tx) error {
		index, err := tx.CreateBucketIfNotExists(indexBucket)
		if err != nil {
			return err
		}

		var keys []*ViewKey
		val := index.Get([]byte(docID))

		if len(val) > 0 {
			err := cbor.Unmarshal(val, &keys)
			if err != nil {
				return err
			}
		}

		for _, key := range keys {
			bucket := tx.Bucket(key.V)
			if bucket == nil {
				continue // view dosn't exist anymore, don't bother deleting
			}

			err = bucket.Delete(key.K)
			if err != nil {
				return err
			}
		}

		return nil
	})
	return err
}
