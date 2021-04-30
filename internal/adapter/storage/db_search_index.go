package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/blevesearch/bleve"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

const SearchDir = "search_indices"
const indexExt = ".bleve"

var _ port.SearchIndex = (port.SearchIndex)(nil)

// OpenSearchIndicies load all created database indicies
func (d *Database) OpenSearchIndicies() error {
	path := filepath.Join(d.databaseDir, SearchDir)

	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read search indices dir %q: %w", path, err)
	}

	dirEntries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to find search indices in %q: %w", path, err)
	}

	for _, entry := range dirEntries {
		if filepath.Ext(entry.Name()) != indexExt {
			continue
		}

		docID := strings.TrimSuffix(entry.Name(), indexExt)
		si, err := d.OpenSearchIndex(docID)
		if err != nil {
			log.Printf("skipping, unable to open saearch index, possible corruption: %v", err)
		}

		d.muSearchIndicies.Lock()
		d.searchIndicies[si.Name()] = si
		d.muSearchIndicies.Unlock()
	}

	return nil
}

func (d *Database) EnsureSearchIndex(docID string) (port.SearchIndex, error) {
	// check if the index already exists
	d.muSearchIndicies.Lock()
	si, ok := d.searchIndicies[docID]
	d.muSearchIndicies.RUnlock()
	if ok {
		return si, nil
	}

	// index doesn't exist, create a new index
	d.muSearchIndicies.Lock()
	defer d.muSearchIndicies.Unlock()

	// try to open search from fs
	si, err := d.OpenSearchIndex(docID)
	if err == nil {
		d.searchIndicies[si.Name()] = si
		return si, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// create new search index for the doc
	si, err = d.CreateSearchIndex(docID)
	if err != nil {
		return nil, err
	}

	d.searchIndicies[si.Name()] = si
	return si, nil
}

func (d *Database) UpdateSearch(ctx context.Context, ddfn *model.DesignDocFn, docs []*model.Document) error {
	return nil
}

func (d *Database) SearchIndex(docID string) string {
	return filepath.Join(d.databaseDir, SearchDir, docID+indexExt)
}

func (d *Database) OpenSearchIndex(docID string) (port.SearchIndex, error) {
	path := d.SearchIndex(docID)
	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open search index %q: %w", path, index)
	}
	return &SearchIndex{
		name: docID,
		idx:  index,
		path: path,
	}, nil
}

func (d *Database) CreateSearchIndex(docID string) (port.SearchIndex, error) {
	path := d.SearchIndex(docID)
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create search index %q: %w", path, index)
	}
	return &SearchIndex{
		name: docID,
		idx:  index,
		path: path,
	}, nil
}

type SearchIndex struct {
	name string
	idx  bleve.Index
	path string
}

func (si SearchIndex) Name() string {
	return si.name
}

func (si SearchIndex) String() string {
	cnt, err := si.idx.DocCount()
	if err != nil {
		log.Printf("failed to get search index %s count: %v", si.path, err)
	}

	return fmt.Sprintf("<SearchIndex name=%q count=%v>", si.name, cnt)
}

func (si *SearchIndex) Close() error {
	return si.idx.Close()
}

func (si *SearchIndex) Index(id string, data interface{}) error {
	return si.idx.Index(id, data)
}

func (si *SearchIndex) Delete(id string) error {
	return si.idx.Delete(id)
}

func (si *SearchIndex) Destroy() error {
	err := si.Close()
	if err != nil {
		log.Printf("failed to close search index %s before destroy: %v", si.path, err)
	}

	return os.RemoveAll(si.path)
}
