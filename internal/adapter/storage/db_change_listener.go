package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"log"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type changeListerner struct {
	key []byte
	ctx context.Context
	cl  port.ChangeListener
}

// AddListener add a change listener to the database changes (document updates)
// the listener will stay registered as long as the context is valid
func (d *Database) AddListener(ctx context.Context, cl port.ChangeListener) error {
	var key [12]byte
	_, err := rand.Read(key[:])
	if err != nil {
		return err
	}
	d.listener.Store(key, &changeListerner{
		key: key[:],
		ctx: ctx,
		cl:  cl,
	})
	return nil
}

// NotifyDocumentUpdate notifies about the change of the passed document
// using a separate goroutine
func (d *Database) NotifyDocumentUpdate(doc *model.Document) {
	go func() {
		var deletionKeys []interface{}
		d.listener.Range(func(k, value interface{}) bool {
			cl := value.(*changeListerner)
			err := cl.cl.DocumentChanged(cl.ctx, doc)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				deletionKeys = append(deletionKeys, k)
				return true
			}
			if err != nil {
				deletionKeys = append(deletionKeys, k)
				log.Printf("failed to update change listener, removing: %v", err)
			}
			return true
		})
		for _, k := range deletionKeys {
			d.listener.Delete(k)
		}
	}()
}
