package handler

import (
	"encoding/json"
	"net/http"

	"github.com/goydb/goydb/pkg/model"
)

type Replicate struct {
	Base
}

func (s *Replicate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	var body struct {
		Source             string                 `json:"source"`
		Target             string                 `json:"target"`
		Continuous         bool                   `json:"continuous"`
		CreateTarget       bool                   `json:"create_target"`
		Cancel             bool                   `json:"cancel"`
		DocIDs             []string               `json:"doc_ids"`
		Filter             string                 `json:"filter"`
		QueryParams        map[string]interface{} `json:"query_params"`
		Selector           map[string]interface{} `json:"selector"`
		SinceSeq           interface{}            `json:"since_seq"`
		UseCheckpoints     *bool                  `json:"use_checkpoints"`
		CheckpointInterval int                    `json:"checkpoint_interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Cancel {
		// Cancel is accepted but we don't have persistent replication IDs
		// to cancel. Return OK for API compatibility.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true}) // nolint: errcheck
		return
	}

	if body.Source == "" || body.Target == "" {
		WriteError(w, http.StatusBadRequest, "source and target are required")
		return
	}

	repDoc := &model.ReplicationDoc{
		Source:       body.Source,
		Target:       body.Target,
		Continuous:   body.Continuous,
		CreateTarget: body.CreateTarget,
		DocIDs:       body.DocIDs,
		Filter:       body.Filter,
		Selector:     body.Selector,
	}

	result, err := s.Replication.RunSync(r.Context(), repDoc)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ // nolint: errcheck
		"ok":                     true,
		"source_last_seq":        result.DocsRead,
		"replication_id_version": 4,
		"history": []map[string]interface{}{
			{
				"docs_read":    result.DocsRead,
				"docs_written": result.DocsWritten,
				"start_time":   result.StartTime.Format("Mon, 02 Jan 2006 15:04:05 GMT"),
				"end_time":     result.EndTime.Format("Mon, 02 Jan 2006 15:04:05 GMT"),
			},
		},
	})
}
