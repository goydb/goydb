package handler

import (
	"encoding/json"
	"net/http"
)

type DBIndex struct {
	Base
}

func (s *DBIndex) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	stats, err := db.Stats(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := DBResponse{
		DbName:      db.Name(),
		DocCount:    stats.DocCount,
		DocDelCount: stats.DocDelCount,
		PurgeSeq:    "0",
		UpdateSeq:   db.Sequence(),
		Sizes: Sizes{
			File:     stats.FileSize,
			Active:   stats.Alloc,
			External: stats.InUse,
		},
		InstanceStartTime: "0", // legacy
		DiskFormatVersion: 8,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type DBResponse struct {
	DbName            string   `json:"db_name"`
	PurgeSeq          string   `json:"purge_seq"`
	UpdateSeq         string   `json:"update_seq"`
	Sizes             Sizes    `json:"sizes"`
	Props             Props    `json:"props"`
	DocDelCount       uint64   `json:"doc_del_count"`
	DocCount          uint64   `json:"doc_count"`
	DiskFormatVersion uint64   `json:"disk_format_version"`
	CompactRunning    bool     `json:"compact_running"`
	Cluster           *Cluster `json:"cluster"`
	InstanceStartTime string   `json:"instance_start_time"`
}
type Sizes struct {
	File     uint64 `json:"file"`
	External uint64 `json:"external"`
	Active   uint64 `json:"active"`
}
type Props struct {
}
type Cluster struct {
	Q int `json:"q"`
	N int `json:"n"`
	W int `json:"w"`
	R int `json:"r"`
}
