package handler

import (
	"path/filepath"

	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
)

type Router struct {
	Storage      port.Storage
	SessionStore sessions.Store
	Admins       model.AdminUsers
	Config       *ConfigStore
	Replication  *service.Replication
	Logger       port.Logger
}

func (router Router) Build(r *mux.Router) error {
	b := Base(router)
	if b.Config == nil {
		configPath := ""
		if b.Storage != nil {
			configPath = filepath.Join(b.Storage.Path(), "_config.json")
		}
		b.Config = NewConfigStore(configPath, b.Logger)
	}

	r.Methods("GET").Path("/_up").Handler(&Up{Base: b})
	r.Methods("GET").Path("/_config").Handler(&ConfigAll{Base: b})
	r.Methods("GET").Path("/_config/{section}").Handler(&ConfigSection{Base: b})
	r.Methods("GET").Path("/_config/{section}/{key}").Handler(&ConfigKey{Base: b})
	r.Methods("PUT").Path("/_config/{section}/{key}").Handler(&ConfigKeyPut{Base: b})
	r.Methods("DELETE").Path("/_config/{section}/{key}").Handler(&ConfigKeyDelete{Base: b})

	// CouchDB 2.x node-scoped config API — same store, node name ignored.
	r.Methods("GET").Path("/_node/{node}/_config").Handler(&ConfigAll{Base: b})
	r.Methods("GET").Path("/_node/{node}/_config/{section}").Handler(&ConfigSection{Base: b})
	r.Methods("GET").Path("/_node/{node}/_config/{section}/{key}").Handler(&ConfigKey{Base: b})
	r.Methods("PUT").Path("/_node/{node}/_config/{section}/{key}").Handler(&ConfigKeyPut{Base: b})
	r.Methods("DELETE").Path("/_node/{node}/_config/{section}/{key}").Handler(&ConfigKeyDelete{Base: b})
	r.Methods("GET").Path("/_membership").Handler(&Membership{Base: b})
	r.Methods("GET").Path("/_cluster_setup").Handler(&ClusterSetupGet{Base: b})
	r.Methods("POST").Path("/_cluster_setup").Handler(&ClusterSetupPost{Base: b})
	r.Methods("GET").Path("/_all_dbs").Handler(&DBAll{Base: b})
	r.Methods("GET").Path("/_uuids").Handler(&UUIDs{})
	r.Methods("GET").Path("/_active_tasks").Handler(&ActiveTasks{Base: b})
	r.Methods("POST").Path("/_replicate").Handler(&Replicate{Base: b})
	r.Methods("GET").Path("/_scheduler/jobs").Handler(&SchedulerJobs{Base: b})
	r.Methods("GET").Path("/_scheduler/docs").Handler(&SchedulerDocs{Base: b})

	r.Methods("GET").Path("/_session").Handler(&SessionGet{Base: b})
	r.Methods("POST").Path("/_session").Handler(&SessionPost{Base: b})
	r.Methods("DELETE").Path("/_session").Handler(&SessionDelete{Base: b})

	r.Methods("POST").Path("/{db}/_ensure_full_commit").Handler(&DBEnsureFullCommit{Base: b})
	r.Methods("POST").Path("/{db}/_compact").Handler(&DBCompact{Base: b})
	r.Methods("POST").Path("/{db}/_compact/{ddoc}").Handler(&DBCompact{Base: b})
	r.Methods("POST").Path("/{db}/_view_cleanup").Handler(&DBViewCleanup{Base: b})

	r.Methods("GET", "POST").Path("/{db}/_all_docs").Handler(&DBDocsAll{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_changes").Handler(&DBChanges{Base: b})
	r.Methods("POST").Path("/{db}/_revs_diff").Handler(&DBRevsDiff{Base: b})
	r.Methods("POST").Path("/{db}/_find").Handler(&DBDocsFind{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_design_docs").Handler(&DBDesignDocs{Base: b})
	r.Methods("POST").Path("/{db}/_bulk_get").Handler(&DBDocsBulkGet{Base: b})
	r.Methods("POST").Path("/{db}/_missing_revs").Handler(&DBMissingRevs{Base: b})

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

	r.Methods("HEAD").Path("/{db}/{docid}").Handler(&DBDocHead{Base: b})
	r.Methods("GET").Path("/{db}/{docid}").Handler(&DBDocGet{Base: b})
	r.Methods("PUT").Path("/{db}/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/{docid}").Handler(&DBDocDelete{Base: b})
	r.Methods("GET").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentGet{Base: b})
	r.Methods("PUT").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentPut{Base: b})
	r.Methods("DELETE").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentDelete{Base: b})

	r.Methods("HEAD").Path("/{db}").Handler(&DBHead{Base: b})
	r.Methods("GET").Path("/{db}/").Handler(&DBIndex{Base: b})
	r.Methods("GET").Path("/{db}").Handler(&DBIndex{Base: b})
	r.Methods("PUT").Path("/{db}").Handler(&DBCreate{Base: b})
	r.Methods("DELETE").Path("/{db}").Handler(&DBDelete{Base: b})
	r.Methods("POST").Path("/{db}").Handler(&DBDocPost{Base: b})

	r.Methods("GET").Path("/").Handler(&Index{})

	return nil
}
