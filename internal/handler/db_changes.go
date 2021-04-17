package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/goydb/goydb/pkg/port"
)

type DBChanges struct {
	Base
}

// FIXME: make config
const maxTimeout = time.Second * 60

func (s *DBChanges) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	query := r.URL.Query()
	includeDocs := boolOption("include_docs", false, query)
	options := port.ChangesOptions{
		Since:   strings.ReplaceAll(query.Get("since"), `"`, ""),
		Limit:   int(intOption("limit", 1000, query)),
		Timeout: durationOption("timeout", time.Millisecond, maxTimeout, query),
	}

	changes, pending, err := db.Changes(r.Context(), &options)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
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

	if feed == "normal" {
		fmt.Fprintln(w, `{"results":[`)
	} else if feed == "continuous" {
		w.Header().Set("Transfer-Encoding", "chunked")
	}

	// print an empty line like couchdb
	if options.Limit == 0 {
		fmt.Fprintln(w, "")
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
				fmt.Fprint(w, `,`)
			}
		}
	}

	if options.Limit == 0 && lastSeq == 0 {
		lastSeq, _ = strconv.ParseUint(options.Since, 10, 64)
	}

	if feed == "normal" {
		fmt.Fprintln(w, `],`)
		fmt.Fprintf(w, `"last_seq":"%d","pending":"%d"}`, lastSeq, pending)
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
