package goydb

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/goydb/goydb/internal/handler"
	"github.com/goydb/goydb/internal/service"
	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
)

// NewLocalClient builds a fresh, throwaway HTTP router over gdb's existing
// storage (reusing its already-configured view/reducer/filter engines) with
// one anonymous admin generated just for this call, and pairs it to a new
// *client.Client via a Unix socketpair (pkg/unixhttp.Pair, through
// pkg/client.NewPaired): no listening socket, no port, nothing another
// process could reach.
//
// name labels this client -- it becomes the anonymous admin's username,
// purely for telling instances apart in logs. No password is needed from
// the caller: one is derived fresh for this call and only ever lives in
// this one throwaway router's memory, never written to any database or
// config store, so nothing accumulates no matter how many times this is
// called against the same gdb. gdb itself keeps running and must be closed
// separately by its owner; closing the returned func only stops this one
// paired connection and its throwaway router.
//
// The returned client supports concurrent requests: each HTTP connection it
// opens gets its own fresh, private socketpair (see pkg/unixhttp.Pair), so
// it can hold a request open (e.g. a long-lived changes-feed watcher) while
// also making other calls on the same client (e.g. writing checkpoints).
//
// Do not pass client.WithHTTPClient in opts -- it would replace the paired
// transport NewPaired sets up; NewPaired panics if it detects this.
func (gdb *Goydb) NewLocalClient(name string, opts ...client.Option) (*client.Client, func() error, error) {
	password := deriveSecret("password", name)

	r := mux.NewRouter()
	err := handler.Router{
		Storage:      gdb.Storage,
		SessionStore: sessions.NewCookieStore([]byte(deriveSecret("cookie", name))),
		Admins:       model.AdminUsers{model.AdminUser{Username: name, Password: password}},
		Replication:  &service.Replication{Storage: gdb.Storage, Logger: gdb.Logger},
		Logger:       gdb.Logger,
	}.Build(r)
	if err != nil {
		return nil, nil, fmt.Errorf("goydb: building local client router: %w", err)
	}

	allOpts := append([]client.Option{client.WithBasicAuth(name, password)}, opts...)
	return client.NewPaired(r, allOpts...)
}

// deriveSecret deterministically derives a hex-encoded secret from purpose
// and name (domain-separated so the password and cookie secret never
// coincide). This doesn't need to be unguessable to an attacker -- the
// actual security boundary is the socketpair itself, a private, unlisted
// file descriptor pair nothing else can reach; this exists only to satisfy
// goydb's HTTP layer, which always requires *some* authenticated session
// with no bypass.
func deriveSecret(purpose, name string) string {
	h := sha256.Sum256([]byte("goydb-local-client:" + purpose + ":" + name))
	return hex.EncodeToString(h[:])
}
