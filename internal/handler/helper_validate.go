package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

// ValidateDocUpdate runs all validate_doc_update functions from all design
// documents in the database. If any validation throws, the error is returned
// immediately (first failure wins).
func ValidateDocUpdate(ctx context.Context, db port.Database, logger port.Logger,
	newDoc, oldDoc *model.Document,
	session *model.Session) error {

	docs, _, err := db.AllDesignDocs(ctx)
	if err != nil {
		return err
	}

	for _, ddoc := range docs {
		fnStr, ok := ddoc.Data["validate_doc_update"].(string)
		if !ok || fnStr == "" {
			continue
		}

		lang := ddoc.Language()
		builder := db.ValidateEngine(lang)
		if builder == nil {
			// No engine for this language — skip.
			logger.Warnf(ctx, "no validate engine for language", "language", lang, "ddoc", ddoc.ID)
			continue
		}

		vs, err := builder(fnStr)
		if err != nil {
			// Compilation error — log and skip to avoid blocking all writes.
			logger.Warnf(ctx, "failed to compile validate_doc_update", "ddoc", ddoc.ID, "error", err)
			continue
		}

		jsNew := docToJSObj(newDoc)
		jsOld := docToJSObj(oldDoc)
		jsUserCtx := userCtxToJS(session, db.Name())
		jsSecObj := secObjToJS(ctx, db)

		if err := vs.ExecuteValidation(ctx, jsNew, jsOld, jsUserCtx, jsSecObj); err != nil {
			return err
		}
	}

	return nil
}

// docToJSObj converts a model.Document to the flat map that CouchDB
// validate_doc_update functions expect.
func docToJSObj(doc *model.Document) map[string]interface{} {
	if doc == nil {
		return nil
	}
	m := make(map[string]interface{}, len(doc.Data)+3)
	for k, v := range doc.Data {
		m[k] = v
	}
	m["_id"] = doc.ID
	if doc.Rev != "" {
		m["_rev"] = doc.Rev
	}
	if doc.Deleted {
		m["_deleted"] = true
	}
	return m
}

// userCtxToJS builds the userCtx object for validate_doc_update.
func userCtxToJS(session *model.Session, dbName string) map[string]interface{} {
	roles := session.Roles
	if roles == nil {
		roles = []string{}
	}
	return map[string]interface{}{
		"name":  session.Name,
		"roles": roles,
		"db":    dbName,
	}
}

// secObjToJS builds the security object for validate_doc_update.
func secObjToJS(ctx context.Context, db port.Database) map[string]interface{} {
	sec, err := db.GetSecurity(ctx)
	if err != nil || sec == nil {
		return map[string]interface{}{
			"admins":  map[string]interface{}{"names": []string{}, "roles": []string{}},
			"members": map[string]interface{}{"names": []string{}, "roles": []string{}},
		}
	}

	toSlice := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}

	return map[string]interface{}{
		"admins": map[string]interface{}{
			"names": toSlice(sec.Admins.Names),
			"roles": toSlice(sec.Admins.Roles),
		},
		"members": map[string]interface{}{
			"names": toSlice(sec.Members.Names),
			"roles": toSlice(sec.Members.Roles),
		},
	}
}

// isLocalDoc returns true if the document ID starts with "_local/".
func isLocalDoc(docID string) bool {
	return strings.HasPrefix(docID, "_local/")
}

// writeValidationError writes the appropriate HTTP error for a VDU error.
// Returns true if an error was written (caller should return).
func writeValidationError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	var forbiddenErr *model.ErrForbidden
	var unauthorizedErr *model.ErrUnauthorized
	if errors.As(err, &forbiddenErr) {
		WriteError(w, http.StatusForbidden, forbiddenErr.Msg)
		return true
	}
	if errors.As(err, &unauthorizedErr) {
		WriteError(w, http.StatusUnauthorized, unauthorizedErr.Msg)
		return true
	}
	WriteError(w, http.StatusInternalServerError, err.Error())
	return true
}
