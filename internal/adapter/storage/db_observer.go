package storage

import (
	"context"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// NotifyDocumentUpdate notifies about the change of the passed document
// using a seperate goroutine
func (d *Database) NotifyDocumentUpdate(doc *model.Document) {
	go func() {
		d.mu.RLock()
		for _, ch := range d.channels {
			ch <- doc
		}
		d.mu.RUnlock()
	}()
}

// NewDocObserver creates a new document observer
func (d *Database) NewDocObserver(ctx context.Context) port.Observer {
	o := &Observer{
		ctx: ctx,
		ch:  make(chan *model.Document),
	}

	d.mu.Lock()
	d.channels = append(d.channels, o.ch)
	d.mu.Unlock()

	return o
}

// Observer a document observer, created using
// NewDocObserver.
type Observer struct {
	ctx context.Context
	ch  chan *model.Document
}

// Close the observer, this will also stop wait
func (o *Observer) Close() {
	close(o.ch)
}

// WaitForDoc with timeout, 0 duration means no timeout
func (o *Observer) WaitForDoc(timeout time.Duration) *model.Document {
	var t <-chan time.Time

	if timeout == time.Duration(0) {
		tc := make(chan time.Time)
		defer close(tc)
		t = tc
	} else {
		t = time.After(timeout)
	}

	// wait until timeout (context)
	//      until passed timeout
	//   or a document
	select {
	case <-o.ctx.Done():
	case <-t:
	case doc := <-o.ch:
		return doc
	}
	return nil
}
