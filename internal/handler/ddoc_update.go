package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	uuid "github.com/satori/go.uuid"
)

func (s *DDUpdateFunction) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close() //nolint:errcheck

	db := Database{Base: s.Base}.Do(w, r)
	if db == nil {
		return
	}

	session, ok := Authenticator{Base: s.Base}.DB(w, r, db)
	if !ok {
		return
	}

	vars := mux.Vars(r)
	ddocID := vars["docid"]
	funcName := vars["func"]
	updateDocID := vars["updatedocid"]

	ctx := r.Context()

	// Load design document
	ddoc, err := db.GetDocument(ctx, "_design/"+ddocID)
	if err != nil || ddoc == nil {
		WriteError(w, http.StatusNotFound, "missing")
		return
	}

	// Find the update function
	fn, found := ddoc.Update(funcName)
	if !found {
		WriteError(w, http.StatusNotFound, "missing function "+funcName+" on design doc _design/"+ddocID)
		return
	}

	// Get the engine for this language
	builder := db.UpdateEngine(ddoc.Language())
	if builder == nil {
		WriteError(w, http.StatusInternalServerError, "no update engine for language "+ddoc.Language())
		return
	}

	// Compile the function
	updateServer, err := builder(fn.UpdateFnCode)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to compile update function: "+err.Error())
		return
	}

	// If updatedocid is set, load the existing document
	var jsDoc map[string]interface{}
	var existingDoc *model.Document
	if updateDocID != "" {
		existingDoc, err = db.GetDocument(ctx, updateDocID)
		if err == nil && existingDoc != nil {
			jsDoc = docToJSObj(existingDoc)
		}
		// If not found, jsDoc stays nil — the function receives null
	}

	// Read request body
	bodyBytes, _ := io.ReadAll(r.Body)
	body := string(bodyBytes)

	// Build the req object
	queryParams := map[string]interface{}{}
	for k, v := range r.URL.Query() {
		if len(v) == 1 {
			queryParams[k] = v[0]
		} else {
			queryParams[k] = v
		}
	}

	reqObj := map[string]interface{}{
		"body":     body,
		"method":   r.Method,
		"query":    queryParams,
		"headers":  headerToMap(r.Header),
		"userCtx":  userCtxToJS(session, db.Name()),
		"secObj":   secObjToJS(ctx, db),
		"id":       updateDocID,
		"uuid":     uuid.NewV4().String(),
		"path":     splitPath(r.URL.Path),
		"peer":     r.RemoteAddr,
		"raw_path": r.URL.RawPath,
	}

	// Execute the update function
	result, err := updateServer.ExecuteUpdate(ctx, jsDoc, reqObj)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var savedDocID string
	var savedRev string

	// If the function returned a document, save it
	if result.Doc != nil {
		// Determine the document ID
		if docID, ok := result.Doc["_id"].(string); ok && docID != "" {
			savedDocID = docID
		} else if updateDocID != "" {
			savedDocID = updateDocID
			result.Doc["_id"] = updateDocID
		} else {
			savedDocID = uuid.NewV4().String()
			result.Doc["_id"] = savedDocID
		}

		// Build document to save
		saveDoc := &model.Document{
			ID:   savedDocID,
			Data: make(map[string]interface{}),
		}
		for k, v := range result.Doc {
			switch k {
			case "_id":
				// already set
			case "_rev":
				if rev, ok := v.(string); ok {
					saveDoc.Rev = rev
				}
			case "_deleted":
				if deleted, ok := v.(bool); ok {
					saveDoc.Deleted = deleted
				}
			default:
				saveDoc.Data[k] = v
			}
		}

		// Run VDU before saving
		if !isLocalDoc(savedDocID) {
			vduErr := ValidateDocUpdate(ctx, db, s.Logger, saveDoc, existingDoc, session)
			if writeValidationError(w, vduErr) {
				return
			}
		}

		// Save the document
		rev, err := db.PutDocument(ctx, saveDoc)
		if err != nil {
			if errors.Is(err, port.ErrConflict) {
				WriteError(w, http.StatusConflict, "Document update conflict.")
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		savedRev = rev
	}

	// Set response headers
	if savedDocID != "" {
		w.Header().Set("X-Couch-Id", savedDocID)
	}
	if savedRev != "" {
		w.Header().Set("X-Couch-Update-NewRev", savedRev)
	}

	// Set custom headers from the response
	for k, v := range result.Headers {
		w.Header().Set(k, v)
	}

	// Determine status code
	statusCode := http.StatusOK
	if result.Doc != nil {
		statusCode = http.StatusCreated
	}
	if result.Code != 0 {
		statusCode = result.Code
	}

	// Write response
	if result.JSON != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(result.JSON) //nolint:errcheck
	} else {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/html;charset=utf-8")
		}
		w.WriteHeader(statusCode)
		w.Write([]byte(result.Body)) //nolint:errcheck
	}
}

// headerToMap converts http.Header to a simple map.
func headerToMap(h http.Header) map[string]interface{} {
	m := make(map[string]interface{}, len(h))
	for k, v := range h {
		if len(v) == 1 {
			m[k] = v[0]
		} else {
			m[k] = v
		}
	}
	return m
}

// splitPath splits a URL path into its non-empty components.
func splitPath(p string) []string {
	var parts []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}
