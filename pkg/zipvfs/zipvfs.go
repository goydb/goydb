package zipvfs

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"

	"golang.org/x/tools/godoc/vfs/httpfs"
	"golang.org/x/tools/godoc/vfs/mapfs"
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
	vfs := make(map[string]string)

	for _, f := range r.File {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}

		if f.FileHeader.FileInfo().IsDir() {
			continue
		}

		data, err := ioutil.ReadAll(r)
		if err != nil {
			return nil, err
		}

		vfs[f.FileHeader.Name] = string(data)
	}

	return httpfs.New(mapfs.New(vfs)), nil
}
