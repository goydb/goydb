package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
)

type DBAll struct {
	Base
}

func (s *DBAll) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	if _, ok := (Authenticator{Base: s.Base, RequiresAdmin: true}.Do(w, r)); !ok {
		return
	}

	names, err := s.Storage.Databases(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sort.Stable(sort.StringSlice(names))

	query := r.URL.Query()
	descending := query.Get("descending") == "true"
	if descending {
		for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
			names[i], names[j] = names[j], names[i]
		}
	}

	// Filter by startkey/endkey (JSON-encoded strings or plain strings).
	if sk := query.Get("startkey"); sk != "" {
		startkey := unquoteJSON(sk)
		idx := 0
		for idx < len(names) {
			if (!descending && names[idx] >= startkey) || (descending && names[idx] <= startkey) {
				break
			}
			idx++
		}
		names = names[idx:]
	}
	if ek := query.Get("endkey"); ek != "" {
		endkey := unquoteJSON(ek)
		idx := len(names)
		for i, name := range names {
			if (!descending && name > endkey) || (descending && name < endkey) {
				idx = i
				break
			}
		}
		names = names[:idx]
	}

	// skip
	if skipStr := query.Get("skip"); skipStr != "" {
		if skip, err := strconv.Atoi(skipStr); err == nil && skip > 0 {
			if skip > len(names) {
				skip = len(names)
			}
			names = names[skip:]
		}
	}

	// limit
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit >= 0 {
			if limit < len(names) {
				names = names[:limit]
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names) // nolint: errcheck
}

