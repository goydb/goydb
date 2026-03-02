package handler

import (
	"encoding/json"
	"net/http"
)

// DBsInfo handles POST /_dbs_info.
// Returns information about multiple databases in a single request.
type DBsInfo struct {
	Base
}

func (s *DBsInfo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	adminOnly := configBool(s.Config, "chttpd", "admin_only_all_dbs", true)
	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: adminOnly}.Do(w, r)); !ok {
		return
	}

	var body struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	results := make([]map[string]interface{}, 0, len(body.Keys))

	for _, name := range body.Keys {
		db, err := s.Storage.Database(ctx, name)
		if err != nil {
			results = append(results, map[string]interface{}{
				"key":   name,
				"error": "not_found",
			})
			continue
		}

		stats, err := db.Stats(ctx)
		if err != nil {
			results = append(results, map[string]interface{}{
				"key":   name,
				"error": "internal_error",
			})
			continue
		}

		seq, err := db.Sequence(ctx)
		if err != nil {
			results = append(results, map[string]interface{}{
				"key":   name,
				"error": "internal_error",
			})
			continue
		}

		info := DBResponse{
			DbName:      db.Name(),
			DocCount:    stats.DocCount,
			DocDelCount: stats.DocDelCount,
			PurgeSeq:    "0",
			UpdateSeq:   seq,
			Sizes: Sizes{
				File:     stats.FileSize,
				Active:   stats.Alloc,
				External: stats.InUse,
			},
			InstanceStartTime: "0",
			DiskFormatVersion: 8,
		}

		results = append(results, map[string]interface{}{
			"key":  name,
			"info": info,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results) // nolint: errcheck
}
