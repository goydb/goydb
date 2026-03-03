package handler

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/goydb/goydb/pkg/model"
)

func init() {
	RegisterAuthHandler("proxy_authentication_handler", func(b Base) AuthHandler {
		return &ProxyAuthHandler{Config: b.Config}
	})
}

// ProxyAuthHandler trusts an upstream reverse-proxy to set headers that
// identify the authenticated user. Optionally verifies an HMAC-SHA1 token.
type ProxyAuthHandler struct {
	Config *ConfigStore
}

func (h *ProxyAuthHandler) Name() string { return "proxy" }

func (h *ProxyAuthHandler) Authenticate(r *http.Request) (*model.Session, bool) {
	usernameHeader := h.configOrDefault("x_auth_username", "X-Auth-CouchDB-UserName")
	rolesHeader := h.configOrDefault("x_auth_roles", "X-Auth-CouchDB-Roles")
	tokenHeader := h.configOrDefault("x_auth_token", "X-Auth-CouchDB-Token")

	username := r.Header.Get(usernameHeader)
	if username == "" {
		return nil, false
	}

	// Verify HMAC token if proxy_use_secret is enabled.
	useSecret, _ := h.Config.Get("couch_httpd_auth", "proxy_use_secret")
	if strings.EqualFold(useSecret, "true") {
		secret, _ := h.Config.Get("couch_httpd_auth", "secret")
		if secret == "" {
			return nil, false
		}
		token := r.Header.Get(tokenHeader)
		if token == "" {
			return nil, false
		}
		mac := hmac.New(sha1.New, []byte(secret))
		mac.Write([]byte(username))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(token)) {
			return nil, false
		}
	}

	var roles []string
	if raw := r.Header.Get(rolesHeader); raw != "" {
		for _, r := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(r); trimmed != "" {
				roles = append(roles, trimmed)
			}
		}
	}
	if roles == nil {
		roles = []string{}
	}

	return &model.Session{
		Name:  username,
		Roles: roles,
	}, true
}

func (h *ProxyAuthHandler) configOrDefault(key, def string) string {
	if v, ok := h.Config.Get("couch_httpd_auth", key); ok && v != "" {
		return v
	}
	return def
}
