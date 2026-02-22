package zipvfs

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
)

func BuildFileSystem(ctx context.Context, zipFile io.Reader) (http.FileSystem, error) {
	var buf bytes.Buffer

	n, err := io.Copy(&buf, zipFile)
	if err != nil {
		return nil, err
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), n)
	if err != nil {
		return nil, err
	}

	return http.FS(zipReaderFS{r}), nil
}

// zipReaderFS wraps a zip.Reader to implement fs.FS
type zipReaderFS struct {
	r *zip.Reader
}

func (z zipReaderFS) Open(name string) (fs.File, error) {
	return z.r.Open(name)
}
