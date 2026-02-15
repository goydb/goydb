package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goydb/goydb/pkg/model"
)

type DBChanges struct {
	Base
}

// FIXME: make config
const maxTimeout = time.Second * 60

func (s *DBChanges) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	query := r.URL.Query()
	includeDocs := boolOption("include_docs", false, query)
	options := model.ChangesOptions{
		Since:   strings.ReplaceAll(query.Get("since"), `"`, ""),
		Limit:   int(intOption("limit", 1000, query)),
		Timeout: durationOption("timeout", time.Millisecond, maxTimeout, query),
	}

	if r.Method == "POST" {
		var body struct {
			DocIDs []string `json:"doc_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			options.DocIDs = body.DocIDs
		}
	}

	changes, pending, err := db.Changes(r.Context(), &options)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if len(options.DocIDs) > 0 {
		allowed := make(map[string]bool, len(options.DocIDs))
		for _, id := range options.DocIDs {
			allowed[id] = true
		}
		filtered := changes[:0]
		for _, doc := range changes {
			if allowed[doc.ID] {
				filtered = append(filtered, doc)
			}
		}
		changes = filtered
	}

	if includeDocs {
		err := db.EnrichDocuments(r.Context(), changes)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	feed := query.Get("feed")
	if feed == "" {
		feed = "normal"
	}

	w.Header().Set("Content-Type", "application/json")

	switch feed {
	case "normal":
		_, _ = fmt.Fprintln(w, `{"results":[`)
	case "continuous":
		w.Header().Set("Transfer-Encoding", "chunked")
	}

	// print an empty line like couchdb
	if options.Limit == 0 {
		_, _ = fmt.Fprintln(w, "")
	}

	var lastSeq uint64
	for i, doc := range changes {
		if lastSeq < doc.LocalSeq {
			lastSeq = doc.LocalSeq
		}
		cd := &ChangeDoc{
			Seq:     strconv.FormatUint(doc.LocalSeq, 10),
			ID:      doc.ID,
			Deleted: doc.Deleted,
			Changes: []Revisions{
				{Rev: doc.Rev},
			},
		}
		if includeDocs {
			cd.Doc = doc.Data
		}
		err := json.NewEncoder(w).Encode(cd)
		if err != nil {
			log.Println(err)
			break
		}
		if feed == "normal" {
			if i < len(changes)-1 {
				_, _ = fmt.Fprint(w, `,`)
			}
		}
	}

	if options.Limit == 0 && lastSeq == 0 {
		lastSeq, _ = strconv.ParseUint(options.Since, 10, 64)
	}

	if feed == "normal" {
		_, _ = fmt.Fprintln(w, `],`)
		_, _ = fmt.Fprintf(w, `"last_seq":"%d","pending":"%d"}`, lastSeq, pending)
	}
}

type ChangeDoc struct {
	Seq     string                 `json:"seq"`
	ID      string                 `json:"id"`
	Changes []Revisions            `json:"changes"`
	Deleted bool                   `json:"deleted,omitempty"`
	Doc     map[string]interface{} `json:"doc,omitempty"`
}

type Revisions struct {
	Rev string `json:"rev"`
}
