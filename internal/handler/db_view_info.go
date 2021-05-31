package handler

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBViewInfo struct {
	Base
}

func (s *DBViewInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := string(model.DesignDocPrefix) + mux.Vars(r)["docid"]
	viewName := mux.Vars(r)["view"]

	doc, err := db.GetDocument(r.Context(), docID) // WIP
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	ddfn := model.DesignDocFn{
		Type:        model.ViewFn,
		DesignDocID: docID,
		FnName:      viewName,
	}

	idx, ok := db.Indices()[ddfn.String()]
	if !ok {
		WriteError(w, http.StatusNotFound, "index not found")
		return
	}

	var stats *model.IndexStats
	err = db.RTransaction(r.Context(), func(tx port.Transaction) error {
		var err error
		stats, err = idx.Stats(r.Context(), tx)
		return err
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := &ViewInfoResponse{}
	response.ViewIndex.Language = doc.Language()
	response.ViewIndex.UpdatesPending.Total = stats.Keys
	response.ViewIndex.Sizes.File = stats.Used

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
