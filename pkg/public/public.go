package public

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/zipvfs"
)

type Public struct {
	Dir string
}

func (p Public) Mount(r *mux.Router) error {
	files, err := ioutil.ReadDir(p.Dir)
	if err != nil {
		return err
	}

	for _, f := range files {
		fullPath := path.Join(p.Dir, f.Name())

		if IsZipFile(f) {
			err := MountZIPFile(r, fullPath)
			if err != nil {
				fmt.Errorf("Unable to serve zip %s due to: %w", fullPath, err)
			}
		} else {
			r.PathPrefix("/" + f.Name() + "/").Handler(http.FileServer(http.Dir(p.Dir)))
		}
	}

	return nil
}

func IsZipFile(f os.FileInfo) bool {
	return !f.IsDir() && path.Ext(f.Name()) == ".zip"
}

func MountZIPFile(r *mux.Router, fullPath string) error {
	f, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer f.Close()

	folder := strings.TrimRight(path.Base(f.Name()), path.Ext(f.Name()))

	return MountZIPReader(r, folder, f)
}

func MountZIPReader(r *mux.Router, folder string, f io.Reader) error {
	vfs, err := zipvfs.BuildFileSystem(context.Background(), f)
	if err != nil {
		return err
	}
	r.PathPrefix("/" + folder + "/").Handler(http.StripPrefix("/"+folder, http.FileServer(vfs)))
	return nil
}

type Container interface {
	FolderName() string
	Reader() io.Reader
}

func MountContainer(r *mux.Router, c Container) error {
	return MountZIPReader(r, c.FolderName(), c.Reader())
}
