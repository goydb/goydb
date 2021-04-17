package handler

import (
	"encoding/json"
	"net/http"
)

type Index struct{}

func (s *Index) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	response := &Info{
		Couchdb: "Welcome",
		Version: "0.1.0",
		GitSha:  "master",
		UUID:    "0dbc95c8-4208-11eb-ad76-00155d4c9c92",
		Features: []string{
			"search",
		},
		Vendor: Vendor{
			Name: "goydb",
		},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type Info struct {
	Couchdb  string   `json:"couchdb"`
	Version  string   `json:"version"`
	GitSha   string   `json:"git_sha"`
	UUID     string   `json:"uuid"`
	Features []string `json:"features"`
	Vendor   Vendor   `json:"vendor"`
}

type Vendor struct {
	Name string `json:"name"`
}
