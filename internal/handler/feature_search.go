//go:build !nosearch

package handler

import "github.com/gorilla/mux"

func init() {
	RegisterFeature("bleve", "search", "nouveau")
	RegisterRouterHook(func(r *mux.Router, b Base) {
		// Server-level search/nouveau analyze endpoints.
		r.Methods("POST").Path("/_search_analyze").Handler(&SearchAnalyze{Base: b})
		r.Methods("POST").Path("/_nouveau_analyze").Handler(&SearchAnalyze{Base: b})

		// Database-level search cleanup endpoints.
		r.Methods("POST").Path("/{db}/_search_cleanup").Handler(&SearchCleanup{Base: b})
		r.Methods("POST").Path("/{db}/_nouveau_cleanup").Handler(&SearchCleanup{Base: b})

		// Design document search/nouveau endpoints.
		r.Methods("GET", "POST").Path("/{db}/_design/{docid}/_search/{index}").Handler(&DBSearch{Base: b})
		r.Methods("GET").Path("/{db}/_design/{docid}/_search_info/{index}").Handler(&SearchInfo{Base: b})
		r.Methods("POST").Path("/{db}/_design/{docid}/_nouveau/{index}").Handler(&NouveauSearch{Base: b})
		r.Methods("GET").Path("/{db}/_design/{docid}/_nouveau_info/{index}").Handler(&NouveauInfo{Base: b})
	})
}
