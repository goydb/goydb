package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const AttachmentDir = "attachments"

func (d *Database) DocDir(docID string) string {
	return filepath.Join(d.databaseDir, AttachmentDir, docID)
}

func (d *Database) DocAttachment(docID, attachment string) string {
	return filepath.Join(d.DocDir(docID), attachment)
}

func (d *Database) AttachmentReader(docID, attachment string) (io.ReadCloser, error) {
	return os.Open(d.DocAttachment(docID, attachment))
}

func (d *Database) PutAttachment(ctx context.Context, docID string, att *model.Attachment) (string, error) {
	defer att.Reader.Close()

	var rev string
	err := d.Transaction(ctx, func(tx port.Transaction) error {
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}

		docDir := d.DocDir(docID)
		err = os.MkdirAll(docDir, 0755)
		if err != nil {
			return err
		}

		filePath := filepath.Join(docDir, att.Filename)
		f, err := os.Create(filePath)
		if err != nil {
			defer os.Remove(filePath) // don't leaf broken files
			return err
		}

		sum := md5.New()

		n, err := io.Copy(f, io.TeeReader(att.Reader, sum))
		if err != nil {
			defer os.Remove(filePath) // don't leaf broken files
			return err
		}

		att.Digest = hex.EncodeToString(sum.Sum(nil))
		att.Length = n
		att.Stub = true

		if doc.Attachments == nil {
			doc.Attachments = make(map[string]*model.Attachment)
		}
		doc.Attachments[att.Filename] = att

		rev, err = tx.PutDocument(ctx, doc)
		return err
	})
	return rev, err
}

func (d *Database) DeleteAttachment(ctx context.Context, docID, name string) (string, error) {
	var rev string
	err := d.Transaction(ctx, func(tx port.Transaction) error {
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}

		var ok bool
		_, ok = doc.Attachments[name]
		if !ok {
			return ErrNotFound
		}

		delete(doc.Attachments, name)

		err = os.Remove(d.DocAttachment(docID, name))
		if !ok {
			return err
		}

		rev, err = tx.PutDocument(ctx, doc)
		return err
	})

	return rev, err
}

func (d *Database) GetAttachment(ctx context.Context, docID, name string) (*model.Attachment, error) {
	var attachment *model.Attachment
	err := d.RTransaction(ctx, func(tx port.Transaction) error {
		var err error
		doc, err := tx.GetDocument(ctx, docID)
		if err != nil {
			return err
		}

		var ok bool
		attachment, ok = doc.Attachments[name]
		if !ok {
			return ErrNotFound
		}
		attachment.Reader, err = d.AttachmentReader(docID, name)
		return err
	})
	if err != nil {
		return nil, err
	}

	return attachment, nil
}
