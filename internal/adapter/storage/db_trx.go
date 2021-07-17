package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var ErrNotFound = errors.New("resource not found")
var ErrConflict = errors.New("rev doesn't match for update")

type Transaction struct {
	Database   *Database
	BucketName []byte
	port.EngineWriteTransaction
}

func (tx *Transaction) SetBucketName(bucketName []byte) {
	tx.BucketName = bucketName
}

func (tx *Transaction) bucket() []byte {
	if tx.BucketName != nil {
		return tx.BucketName
	} else {
		return model.DocsBucket
	}
}

func (tx *Transaction) GetRaw(ctx context.Context, key []byte, value interface{}) error {
	data, err := tx.Get(tx.bucket(), key)
	if err != nil {
		return err
	}

	err = bson.Unmarshal(data, value)
	if err != nil {
		return err
	}

	return nil
}

func (tx *Transaction) PutRaw(ctx context.Context, key []byte, raw interface{}) error {
	data, err := bson.Marshal(raw)
	if err != nil {
		return err
	}
	tx.Put(tx.bucket(), key, data)
	return nil
}

func (tx *Transaction) PutDocument(ctx context.Context, doc *model.Document) (rev string, err error) {
	// verify that the transaction is valid for update
	oldDoc, err := tx.GetDocument(ctx, doc.ID)
	if err == nil && oldDoc != nil { // find if there is already a document
		if !oldDoc.ValidUpdateRevision(doc) {
			return "", ErrConflict
		}
	}

	// find next sequences (rev, changes)
	revSeq := doc.NextSequence()
	doc.LocalSeq, err = tx.NextSequence()
	if err != nil {
		return
	}

	hash := md5.New()
	err = cbor.NewEncoder(hash).Encode(doc)
	if err != nil {
		return
	}
	rev = strconv.Itoa(revSeq) + "-" + hex.EncodeToString(hash.Sum(nil))
	doc.Rev = rev

	if oldDoc != nil {
		// maintain indices - remove old value
		for _, index := range tx.Database.Indices() {
			err := index.DocumentDeleted(ctx, tx, oldDoc)
			if err != nil {
				return "", err
			}
		}
	}

	err = tx.PutRaw(ctx, []byte(doc.ID), doc)
	if err != nil {
		return
	}

	if doc.IsDesignDoc() {
		err = tx.Database.BuildDesignDocIndices(ctx, tx, doc, true)
		if err != nil {
			return
		}
	}

	// maintain Indices - add new value
	for _, index := range tx.Database.Indices() {
		err = index.DocumentStored(ctx, tx, doc)
		if err != nil {
			return
		}
	}

	tx.Database.NotifyDocumentUpdate(doc)

	return
}

func (tx *Transaction) GetDocument(ctx context.Context, docID string) (*model.Document, error) {
	var doc model.Document

	err := tx.GetRaw(ctx, []byte(docID), &doc)
	if err == port.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.Data == nil {
		doc.Data = make(map[string]interface{})
	}
	doc.Data["_id"] = doc.ID
	doc.Data["_rev"] = doc.Rev
	if doc.Deleted {
		doc.Data["_deleted"] = true
	}
	if len(doc.Attachments) > 0 {
		doc.Data["_attachments"] = doc.Attachments
	}

	return &doc, nil
}

func (tx *Transaction) DeleteDocument(ctx context.Context, docID, rev string) (*model.Document, error) {
	doc := &model.Document{
		ID:      docID,
		Rev:     rev,
		Deleted: true,
	}

	_, err := tx.PutDocument(ctx, doc)
	if err != nil {
		return doc, err
	}
	return doc, err
}

/*func (tx *Transaction) NextSequence() (uint64, error) {
	bucket, err := tx.tx.CreateBucketIfNotExists(model.DocsBucket)
	if err != nil {
		return 0, err
	}
	seq, err := bucket.NextSequence()
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// Sequence returns the current sequence
func (tx *Transaction) Sequence() uint64 {
	bucket := tx.tx.Bucket(model.DocsBucket)
	if bucket == nil {
		return 0
	}
	return bucket.Sequence()
}*/
