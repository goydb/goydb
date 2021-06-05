package handler

import (
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

type Router struct {
	Storage      port.Storage
	SessionStore sessions.Store
	Admins       model.AdminUsers
}

func (router Router) Build(r *mux.Router) error {
	b := Base{
		Storage:      router.Storage,
		SessionStore: router.SessionStore,
		Admins:       router.Admins,
	}

	r.Methods("GET").Path("/_all_dbs").Handler(&DBAll{Base: b})
	r.Methods("GET").Path("/_uuids").Handler(&UUIDs{})
	r.Methods("GET").Path("/_active_tasks").Handler(&ActiveTasks{Base: b})

	r.Methods("GET").Path("/_session").Handler(&SessionGet{Base: b})
	r.Methods("POST").Path("/_session").Handler(&SessionPost{Base: b})
	r.Methods("DELETE").Path("/_session").Handler(&SessionDelete{Base: b})

	r.Methods("POST").Path("/{db}/_ensure_full_commit").Handler(&DBEnsureFullCommit{Base: b})

	r.Methods("GET").Path("/{db}/_all_docs").Handler(&DBDocsAll{Base: b})
	r.Methods("GET").Path("/{db}/_changes").Handler(&DBChanges{Base: b})
	r.Methods("POST").Path("/{db}/_find").Handler(&DBDocsFind{Base: b})

	r.Methods("GET").Path("/{db}/_security").Handler(&DBSecurityGet{Base: b})
	r.Methods("PUT").Path("/{db}/_security").Handler(&DBSecurityPut{Base: b})

	r.Methods("GET").Path("/{db}/_design/{docid}/_view/{view}").Handler(&DBView{Base: b})
	r.Methods("GET").Path("/{db}/_design/{docid}/_search/{index}").Handler(&DBSearch{Base: b})
	r.Methods("GET").Path("/{db}/_design/{docid}/_info").Handler(&DBIndexInfo{Base: b})
	r.Methods("GET").Path("/{db}/_design/{docid}").Handler(&DBDocGet{Base: b, Design: true})
	r.Methods("PUT").Path("/{db}/_design/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/_design/{docid}").Handler(&DBDocDelete{Base: b, Design: true})

	r.Methods("GET").Path("/{db}/_local_docs").Handler(&DBDocsAll{Base: b, Local: true})
	r.Methods("GET").Path("/{db}/_local/{docid}").Handler(&DBDocGet{Base: b, Local: true})
	r.Methods("PUT").Path("/{db}/_local/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/_local/{docid}").Handler(&DBDocDelete{Base: b, Local: true})

	r.Methods("PUT", "POST").Path("/{db}/_bulk_docs").Handler(&DBDocsBulk{Base: b})

	r.Methods("GET").Path("/{db}/{docid}").Handler(&DBDocGet{Base: b})
	r.Methods("PUT").Path("/{db}/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/{docid}").Handler(&DBDocDelete{Base: b})
	r.Methods("GET").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentGet{Base: b})
	r.Methods("PUT").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentPut{Base: b})
	r.Methods("DELETE").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentDelete{Base: b})

	r.Methods("GET").Path("/{db}/").Handler(&DBIndex{Base: b})
	r.Methods("GET").Path("/{db}").Handler(&DBIndex{Base: b})
	r.Methods("PUT").Path("/{db}").Handler(&DBCreate{Base: b})
	r.Methods("DELETE").Path("/{db}").Handler(&DBDelete{Base: b})

	r.Methods("GET").Path("/").Handler(&Index{})

	return nil
}
