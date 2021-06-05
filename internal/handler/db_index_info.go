package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBIndexInfo struct {
	Base
}

func (s *DBIndexInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := string(model.DesignDocPrefix) + mux.Vars(r)["docid"]
	// TODO: viewName := mux.Vars(r)["view"]

	doc, err := db.GetDocument(r.Context(), docID)
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	response := &ViewInfoResponse{}
	response.ViewIndex.Language = doc.Language()

	err = db.RTransaction(r.Context(), func(tx port.Transaction) error {
		for _, fn := range doc.Functions() {
			idx, ok := db.Indices()[fn.DesignDocFn().String()]
			if ok {
				stat, err := idx.Stats(r.Context(), tx)
				if err != nil {
					return err
				}
				log.Println(stat.String())
				response.ViewIndex.Sizes.Active += stat.Allocated
				response.ViewIndex.Sizes.External += stat.Used
			}
		}
		return nil
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type ViewInfoResponse struct {
	Name      string    `json:"name"`
	ViewIndex ViewIndex `json:"view_index"`
}
type UpdatesPending struct {
	Minimum   uint64 `json:"minimum"`
	Preferred uint64 `json:"preferred"`
	Total     uint64 `json:"total"`
}
type ViewSizes struct {
	File     uint64 `json:"file"`
	External uint64 `json:"external"`
	Active   uint64 `json:"active"`
}
type ViewIndex struct {
	UpdatesPending UpdatesPending `json:"updates_pending"`
	WaitingCommit  bool           `json:"waiting_commit"`
	WaitingClients int            `json:"waiting_clients"`
	UpdaterRunning bool           `json:"updater_running"`
	UpdateSeq      int            `json:"update_seq"`
	Sizes          ViewSizes      `json:"sizes"`
	Signature      string         `json:"signature"`
	PurgeSeq       int            `json:"purge_seq"`
	Language       string         `json:"language"`
	CompactRunning bool           `json:"compact_running"`
}
