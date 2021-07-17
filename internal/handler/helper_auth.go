package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/goydb/goydb/internal/adapter/storage"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
)

type Authenticator struct {
	Base
	RequiresAdmin bool
}

func (a Authenticator) Authenticate(ctx context.Context, username, password string) port.SessionBuilder {
	admin := a.Admins.Authenticate(username, password)
	if admin != nil {
		return admin
	}

	// try to find user document
	var sb port.SessionBuilder
	db, err := a.Base.Storage.Database(ctx, "_users")
	if err != nil {
		log.Println("failed to load users", err)
		return nil
	}
	db.Transaction(ctx, func(tx *storage.Transaction) error {
		doc, err := tx.GetDocument(ctx, "org.couchdb.user:"+username)
		if err != nil {
			return err
		}
		var u model.User
		err = u.FromDocument(doc)
		if err != nil {
			return err
		}
		ok, err := u.VerifyPassword(password)
		if err != nil {
			return err
		}
		if ok {
			sb = &u
		}

		return nil
	})
	if err != nil {
		log.Println("failed to load users", err)
		return nil
	}
	if sb != nil {
		return sb
	}

	return nil
}

func (a Authenticator) Auth(r *http.Request) (*model.Session, string) {
	var s model.Session
	var via string

	session, err := a.SessionStore.Get(r, sessionName)
	if err != nil {
		return nil, ""
	}

	if !session.IsNew {
		s.Restore(session.Values)
		via = "cookie"
	}

	if !s.Authenticated() {
		username, password, ok := r.BasicAuth()
		if ok {
			user := a.Authenticate(r.Context(), username, password)
			if user != nil {
				return user.Session(), "default"
			}
		}
	}

	return &s, via
}

func (a Authenticator) Do(w http.ResponseWriter, r *http.Request) (*model.Session, bool) {
	s, _ := a.Auth(r)
	if s == nil {
		WriteError(w, http.StatusBadRequest, "session is invalid")
		return nil, false
	}

	if !s.Authenticated() {
		if a.RequiresAdmin {
			WriteError(w, http.StatusUnauthorized, "You are not authorized as a server admin.")
			return nil, false
		} else {
			WriteError(w, http.StatusUnauthorized, "You are not authorized to access this db.")
			return nil, false
		}
	}

	if a.RequiresAdmin && !s.IsServerAdmin() {
		WriteError(w, http.StatusUnauthorized, "You are not a server admin.")
		return nil, false
	}

	return s, true
}

func (a Authenticator) DB(w http.ResponseWriter, r *http.Request, db *storage.Database) (*model.Session, bool) {
	s, ok := a.Do(w, r)
	if !ok {
		return nil, false
	}

	sec, err := db.GetSecurity(r.Context())
	if err != nil {
		return nil, false
	}

	var names []string
	var roles []string

	if a.RequiresAdmin {
		names = sec.Admins.Names
		roles = sec.Admins.Roles
	} else {
		names = sec.Members.Names
		roles = sec.Members.Roles
	}

	for _, name := range names {
		if name == s.Name {
			return s, true
		}
	}

	for _, role := range roles {
		for _, srole := range s.Roles {
			if role == srole {
				return s, true
			}
		}
	}

	WriteError(w, http.StatusUnauthorized, "You are not authorized to access this db.")
	return nil, false
}
