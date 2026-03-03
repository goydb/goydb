package handler

import (
	"net/http"
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
	r.Methods("POST").Path("/_node/{node}/_config/_reload").Handler(&ConfigReload{Base: b})
	r.Methods("GET").Path("/_node/{node}/_stats").Handler(&NodeStats{Base: b})
	r.Methods("GET").Path("/_node/{node}/_system").Handler(&NodeSystem{Base: b})
	r.Methods("POST").Path("/_node/{node}/_restart").Handler(&NodeRestart{Base: b})
	r.Methods("GET").Path("/_node/{node}/_versions").Handler(&NodeVersions{Base: b})
	r.Methods("GET").Path("/_node/{node}/_smoosh/status").Handler(&NodeSmooshStatus{Base: b})
	r.Methods("GET").Path("/_node/{node}").Handler(&NodeInfo{Base: b})
	r.Methods("GET").Path("/_membership").Handler(&Membership{Base: b})
	r.Methods("GET").Path("/_cluster_setup").Handler(&ClusterSetupGet{Base: b})
	r.Methods("POST").Path("/_cluster_setup").Handler(&ClusterSetupPost{Base: b})
	r.Methods("GET").Path("/_db_updates").Handler(&DBUpdates{Base: b})
	r.Methods("GET").Path("/_all_dbs").Handler(&DBAll{Base: b})
	r.Methods("POST").Path("/_dbs_info").Handler(&DBsInfo{Base: b})
	r.Methods("GET").Path("/_uuids").Handler(&UUIDs{})
	r.Methods("GET").Path("/_active_tasks").Handler(&ActiveTasks{Base: b})
	r.Methods("POST").Path("/_replicate").Handler(&Replicate{Base: b})
	r.Methods("GET").Path("/_scheduler/jobs").Handler(&SchedulerJobs{Base: b})
	r.Methods("GET").Path("/_scheduler/docs").Handler(&SchedulerDocs{Base: b})
	r.Methods("GET").Path("/_scheduler/docs/{repid}").Handler(&SchedulerDocByID{Base: b})

	r.Methods("GET").Path("/_reshard").Handler(&ReshardGet{Base: b})
	r.Methods("GET", "PUT").Path("/_reshard/state").Handler(&ReshardState{Base: b})
	r.Methods("GET", "POST").Path("/_reshard/jobs").Handler(&ReshardJobs{Base: b})

	r.Methods("GET").Path("/_session").Handler(&SessionGet{Base: b})
	r.Methods("POST").Path("/_session").Handler(&SessionPost{Base: b})
	r.Methods("DELETE").Path("/_session").Handler(&SessionDelete{Base: b})

	r.Methods("POST").Path("/{db}/_ensure_full_commit").Handler(&DBEnsureFullCommit{Base: b})
	r.Methods("POST").Path("/{db}/_compact").Handler(&DBCompact{Base: b})
	r.Methods("POST").Path("/{db}/_compact/{ddoc}").Handler(&DBCompact{Base: b})
	r.Methods("POST").Path("/{db}/_view_cleanup").Handler(&DBViewCleanup{Base: b})

	r.Methods("POST").Path("/{db}/_all_docs/queries").Handler(&DBDocsQueries{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_all_docs").Handler(&DBDocsAll{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_changes").Handler(&DBChanges{Base: b})
	r.Methods("POST").Path("/{db}/_revs_diff").Handler(&DBRevsDiff{Base: b})
	r.Methods("GET").Path("/{db}/_shards").Handler(&DBShards{Base: b})
	r.Methods("GET").Path("/{db}/_shards/{docid}").Handler(&DBShardsDoc{Base: b})
	r.Methods("POST").Path("/{db}/_sync_shards").Handler(&DBSyncShards{Base: b})
	r.Methods("POST").Path("/{db}/_purge").Handler(&DBPurge{Base: b})
	r.Methods("GET").Path("/{db}/_purged_infos_limit").Handler(&DBPurgedInfosLimitGet{Base: b})
	r.Methods("PUT").Path("/{db}/_purged_infos_limit").Handler(&DBPurgedInfosLimitPut{Base: b})
	r.Methods("POST").Path("/{db}/_find").Handler(&DBDocsFind{Base: b})
	r.Methods("POST").Path("/{db}/_explain").Handler(&DBDocsExplain{Base: b})
	r.Methods("POST").Path("/{db}/_index").Handler(&DBIndexPost{Base: b})
	r.Methods("GET").Path("/{db}/_index").Handler(&DBIndexGet{Base: b})
	r.Methods("DELETE").Path("/{db}/_index/{ddoc}/json/{name}").Handler(&DBIndexDelete{Base: b})
	r.Methods("POST").Path("/{db}/_design_docs/queries").Handler(&DBDesignDocsQueries{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_design_docs").Handler(&DBDesignDocs{Base: b})
	r.Methods("POST").Path("/{db}/_bulk_get").Handler(&DBDocsBulkGet{Base: b})
	r.Methods("POST").Path("/{db}/_missing_revs").Handler(&DBMissingRevs{Base: b})

	r.Methods("GET").Path("/{db}/_security").Handler(&DBSecurityGet{Base: b})
	r.Methods("PUT").Path("/{db}/_security").Handler(&DBSecurityPut{Base: b})

	r.Methods("GET").Path("/{db}/_revs_limit").Handler(&DBRevsLimitGet{Base: b})
	r.Methods("PUT").Path("/{db}/_revs_limit").Handler(&DBRevsLimitPut{Base: b})

	r.Methods("POST").Path("/{db}/_design/{docid}/_view/{view}/queries").Handler(&DBViewQueries{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_design/{docid}/_view/{view}").Handler(&DBView{Base: b})
	for _, hook := range routerHooks {
		hook(r, b)
	}

	r.Methods("GET", "POST").Path("/{db}/_design/{docid}/_show/{func}").Handler(&DDShowFunction{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_design/{docid}/_show/{func}/{showdocid}").Handler(&DDShowFunction{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_design/{docid}/_list/{func}/{view}").Handler(&DDListFunction{Base: b})
	r.Methods("POST", "PUT").Path("/{db}/_design/{docid}/_update/{func}").Handler(&DDUpdateFunction{Base: b})
	r.Methods("POST", "PUT").Path("/{db}/_design/{docid}/_update/{func}/{updatedocid}").Handler(&DDUpdateFunction{Base: b})
	r.PathPrefix("/{db}/_design/{docid}/_rewrite/").Handler(&DDRewrite{Base: b})
	r.Methods("GET").Path("/{db}/_design/{docid}/_info").Handler(&DBIndexInfo{Base: b})
	r.Methods("HEAD").Path("/{db}/_design/{docid}").Handler(&DBDocHead{Base: b, Design: true})
	r.Methods("GET").Path("/{db}/_design/{docid}").Handler(&DBDocGet{Base: b, Design: true})
	r.Methods("PUT").Path("/{db}/_design/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/_design/{docid}").Handler(&DBDocDelete{Base: b, Design: true})
	r.Methods("COPY").Path("/{db}/_design/{docid}").Handler(&DBDocCopy{Base: b, Design: true})
	r.Methods("HEAD").Path("/{db}/_design/{docid}/{attachment}").Handler(&DBDocAttachmentHead{Base: b, Design: true})
	r.Methods("GET").Path("/{db}/_design/{docid}/{attachment}").Handler(&DBDocAttachmentGet{Base: b, Design: true})
	r.Methods("PUT").Path("/{db}/_design/{docid}/{attachment}").Handler(&DBDocAttachmentPut{Base: b, Design: true})
	r.Methods("DELETE").Path("/{db}/_design/{docid}/{attachment}").Handler(&DBDocAttachmentDelete{Base: b, Design: true})

	r.Methods("GET").Path("/{db}/_partition/{partition}/_design/{ddoc}/_view/{view}").Handler(&PartitionView{Base: b})
	r.Methods("POST").Path("/{db}/_partition/{partition}/_find").Handler(&PartitionFind{Base: b})
	r.Methods("POST").Path("/{db}/_partition/{partition}/_explain").Handler(&PartitionExplain{Base: b})
	r.Methods("GET").Path("/{db}/_partition/{partition}/_all_docs").Handler(&PartitionAllDocs{Base: b})
	r.Methods("GET").Path("/{db}/_partition/{partition}").Handler(&PartitionInfo{Base: b})

	r.Methods("POST").Path("/{db}/_local_docs/queries").Handler(&DBLocalDocsQueries{Base: b})
	r.Methods("GET", "POST").Path("/{db}/_local_docs").Handler(&DBDocsAll{Base: b, Local: true})
	r.Methods("GET").Path("/{db}/_local/{docid}").Handler(&DBDocGet{Base: b, Local: true})
	r.Methods("PUT").Path("/{db}/_local/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/_local/{docid}").Handler(&DBDocDelete{Base: b, Local: true})
	r.Methods("COPY").Path("/{db}/_local/{docid}").Handler(&DBDocCopy{Base: b, Local: true})

	r.Methods("PUT", "POST").Path("/{db}/_bulk_docs").Handler(&DBDocsBulk{Base: b})

	r.Methods("HEAD").Path("/{db}/{docid}").Handler(&DBDocHead{Base: b})
	r.Methods("GET").Path("/{db}/{docid}").Handler(&DBDocGet{Base: b})
	r.Methods("PUT").Path("/{db}/{docid}").Handler(&DBDocPut{Base: b})
	r.Methods("DELETE").Path("/{db}/{docid}").Handler(&DBDocDelete{Base: b})
	r.Methods("COPY").Path("/{db}/{docid}").Handler(&DBDocCopy{Base: b})
	r.Methods("HEAD").Path("/{db}/{docid}/{attachment}").Handler(&DBDocAttachmentHead{Base: b})
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

	// Middleware: enforce max_http_request_size on all requests.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			limit := configInt64(b.Config, "chttpd", "max_http_request_size")
			if limit > 0 && req.Body != nil {
				req.Body = http.MaxBytesReader(w, req.Body, limit)
			}
			next.ServeHTTP(w, req)
		})
	})

	return nil
}
