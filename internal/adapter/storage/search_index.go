package storage

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve"
	"github.com/goydb/goydb/pkg/port"
)

const SearchDir = "search_indices"

var _ port.SearchIndex = (port.SearchIndex)(nil)

type SearchIndex struct {
	idx  bleve.Index
	path string
}

func (d *Database) SearchDir(docID, view string) string {
	return filepath.Join(d.databaseDir, SearchDir, docID+"."+view+".bleve")
}

func (d *Database) OpenSearchIndex(path string) (port.SearchIndex, error) {
	index, err := bleve.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open search index %q: %w", path, index)
	}
	return &SearchIndex{
		idx:  index,
		path: path,
	}, nil
}

func (d *Database) CreateSearchIndex(path string) (port.SearchIndex, error) {
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(path, mapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create search index %q: %w", path, index)
	}
	return &SearchIndex{
		idx:  index,
		path: path,
	}, nil
}

func (si *SearchIndex) String() string {
	cnt, err := si.idx.DocCount()
	if err != nil {
		log.Printf("failed to get search index %s count: %v", si.path, err)
	}

	return fmt.Sprintf("<SearchIndex path=%q count=%v>", si.path, cnt)
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
