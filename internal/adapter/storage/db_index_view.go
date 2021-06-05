package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/fxamacker/cbor/v2"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"gopkg.in/mgo.v2/bson"
)

var _ port.DocumentIndex = (*ViewIndex)(nil)
var _ port.DocumentIndexSourceUpdate = (*ViewIndex)(nil)

type ViewIndex struct {
	*RegularIndex
	MapFn   string
	engines port.ViewEngines
	server  port.ViewServer
	mu      sync.RWMutex
}

func NewViewIndex(ddfn *model.DesignDocFn, engines port.ViewEngines) *ViewIndex {
	vi := &ViewIndex{
		engines: engines,
	}

	vi.RegularIndex = NewRegularIndex(ddfn, vi.indexSingleDocument)
	vi.RegularIndex.cleanKey = func(b []byte) string {
		var i interface{}
		err := cbor.Unmarshal(b, &i)
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%v", i)
	}

	return vi
}

func (i *ViewIndex) String() string {
	return fmt.Sprintf("<ViewIndex name=%q>", i.RegularIndex.ddfn)
}

func (i *ViewIndex) indexSingleDocument(ctx context.Context, doc *model.Document) ([][]byte, [][]byte) {
	// ignore deleted documents
	if doc.Deleted {
		return nil, nil
	}

	// get view server
	i.mu.RLock()
	vs := i.server
	i.mu.RUnlock()

	// execute document against view server
	docs, err := vs.ExecuteView(ctx, []*model.Document{doc})
	if err != nil {
		log.Printf("Failed to execute view: %v", err)
		return nil, nil
	}

	var keys, values [][]byte

	// index all values
	for _, doc := range docs {
		key, err := cbor.Marshal(doc.Key)
		if err != nil {
			log.Printf("Failed to marshal key: %v", err)
			return nil, nil
		}
		keys = append(keys, key)
		out, err := bson.Marshal(doc)
		if err == nil {
			values = append(values, out)
		}
	}

	return keys, values
}

// updateSource updates the view source and starts
// rebuilding the whole index
func (i *ViewIndex) UpdateSource(ctx context.Context, doc *model.Document, vf *model.Function) error {
	mapFn := vf.MapFn
	language := doc.Language()

	if mapFn == "" {
		return errors.New("invalid empty view function")
	}

	// if the mapFn is the same, to nothing
	if i.MapFn == mapFn {
		return nil
	}

	// func is different, build the view server
	builder, ok := i.engines[language]
	if !ok {
		return fmt.Errorf("view engine for language %q is not registered", language)
	}
	vs, err := builder(mapFn)
	if err != nil {
		return fmt.Errorf("failed to compile view: %w", err)
	}

	// view was successfully created, update mapFn and viewServer
	i.mu.Lock()
	i.MapFn = mapFn
	i.server = vs
	i.mu.Unlock()

	return nil
}

func (i *ViewIndex) SourceType() model.FnType {
	return model.ViewFn
}
