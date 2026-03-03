package handler

import (
	"context"
	"net/http"

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
	db, err := a.Storage.Database(ctx, "_users")
	if err != nil {
		a.Logger.Warnf(ctx, "failed to load users database", "error", err)
		return nil
	}
	doc, err := db.GetDocument(ctx, "org.couchdb.user:"+username)
	if err != nil {
		a.Logger.Warnf(ctx, "failed to load user document", "username", username, "error", err)
		return nil
	}
	if doc != nil {
		var u model.User
		err = u.FromDocument(doc)
		if err != nil {
			a.Logger.Warnf(ctx, "failed to parse user document", "error", err)
			return nil
		}
		ok, err := u.VerifyPassword(password)
		if err != nil {
			a.Logger.Warnf(ctx, "failed to verify password", "error", err)
			return nil
		}
		if ok {
			sb = &u
		}
	}

	return sb
}

// AuthenticateWithUser is like Authenticate but also returns the *model.User
// when authenticated via the _users database. Returns nil user for server
// admins (they have no user document).
func (a Authenticator) AuthenticateWithUser(ctx context.Context, username, password string) (port.SessionBuilder, *model.User) {
	admin := a.Admins.Authenticate(username, password)
	if admin != nil {
		return admin, nil
	}

	db, err := a.Storage.Database(ctx, "_users")
	if err != nil {
		a.Logger.Warnf(ctx, "failed to load users database", "error", err)
		return nil, nil
	}
	doc, err := db.GetDocument(ctx, "org.couchdb.user:"+username)
	if err != nil {
		a.Logger.Warnf(ctx, "failed to load user document", "username", username, "error", err)
		return nil, nil
	}
	if doc != nil {
		var u model.User
		err = u.FromDocument(doc)
		if err != nil {
			a.Logger.Warnf(ctx, "failed to parse user document", "error", err)
			return nil, nil
		}
		ok, err := u.VerifyPassword(password)
		if err != nil {
			a.Logger.Warnf(ctx, "failed to verify password", "error", err)
			return nil, nil
		}
		if ok {
			return &u, &u
		}
	}

	return nil, nil
}

func (a Authenticator) Auth(r *http.Request) (*model.Session, string) {
	for _, h := range buildAuthChain(a.Base) {
		if s, ok := h.Authenticate(r); ok {
			return s, h.Name()
		}
	}
	return &model.Session{}, ""
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

func (a Authenticator) DB(w http.ResponseWriter, r *http.Request, db port.Database) (*model.Session, bool) {
	s, ok := a.Do(w, r)
	if !ok {
		return nil, false
	}

	sec, err := db.GetSecurity(r.Context())
	if err != nil {
		a.Logger.Errorf(r.Context(), "failed to get security", "error", err)
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

	// CouchDB semantics: empty Members = any authenticated user may access.
	// Do() above already verified authentication.
	if !a.RequiresAdmin && len(names) == 0 && len(roles) == 0 {
		return s, true
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
