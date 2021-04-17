package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/internal/controller"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type DBView struct {
	Base
}

func (s *DBView) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	if _, ok := (Authenticator{Base: s.Base}.DB(w, r, db)); !ok {
		return
	}

	docID := "_design/" + mux.Vars(r)["docid"]
	viewName := mux.Vars(r)["view"]

	doc, err := db.GetDocument(r.Context(), docID) // WIP
	if err != nil {
		WriteError(w, http.StatusNotFound, err.Error())
		return
	}

	options := r.URL.Query()
	var update string
	if len(options["update"]) > 0 {
		update = options["update"][0]
	}

	switch update {
	case "", "true":
		// wait for all view updates to take place
		for {
			n, err := db.TaskCount(r.Context())
			if err != nil {
				log.Println(err)
				WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if n == 0 {
				break
			}
			time.Sleep(time.Second)
		}
	case "lazy":
		err = db.AddTasks(r.Context(), []*model.Task{
			&model.Task{
				Action:    model.ActionUpdateView,
				DBName:    db.Name(),
				ViewDocID: docID,
			},
		})
	case "false": // do nothing
	}
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var q port.AllDocsQuery
	q.Skip = intOption("skip", 0, options)
	q.Limit = intOption("limit", 0, options)
	q.ViewName = viewName
	q.IncludeDocs = boolOption("include_docs", false, options)
	q.ViewGroup = boolOption("group", false, options)

	var total int
	var docs []*model.Document
	if boolOption("reduce", true, options) {
		docs, total, err = controller.View{
			DB:       db,
			ViewDoc:  doc,
			ViewName: viewName,
		}.ReduceDocs(r.Context(), q)
	} else {
		docs, total, err = db.AllDocs(r.Context(), q)
	}

	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	response := AllDocsResponse{
		TotalRows: total,
		Rows:      make([]Rows, len(docs)),
	}

	for i, doc := range docs {
		response.Rows[i].ID = doc.ID
		response.Rows[i].Key = doc.Key
		response.Rows[i].Value = doc.Value
		if q.IncludeDocs && doc.Data != nil {
			response.Rows[i].Doc = doc.Data
			response.Rows[i].Doc["_id"] = doc.ID
			response.Rows[i].Doc["_rev"] = doc.Rev
			if doc.Deleted {
				response.Rows[i].Doc["_deleted"] = doc.Deleted
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
