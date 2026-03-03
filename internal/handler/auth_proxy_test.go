package handler

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProxyConfig() *ConfigStore {
	return NewConfigStore("", nil)
}

func makeHMAC(username, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(username))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestProxyAuth_UsernameOnly(t *testing.T) {
	cfg := newProxyConfig()
	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "alice")

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "alice", s.Name)
	assert.Empty(t, s.Roles)
}

func TestProxyAuth_WithRoles(t *testing.T) {
	cfg := newProxyConfig()
	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "bob")
	req.Header.Set("X-Auth-CouchDB-Roles", "reader,writer")

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "bob", s.Name)
	assert.Equal(t, []string{"reader", "writer"}, s.Roles)
}

func TestProxyAuth_ValidHMAC(t *testing.T) {
	cfg := newProxyConfig()
	cfg.Set("couch_httpd_auth", "proxy_use_secret", "true")
	cfg.Set("couch_httpd_auth", "secret", "mysecret")

	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "alice")
	req.Header.Set("X-Auth-CouchDB-Token", makeHMAC("alice", "mysecret"))

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "alice", s.Name)
}

func TestProxyAuth_InvalidHMAC(t *testing.T) {
	cfg := newProxyConfig()
	cfg.Set("couch_httpd_auth", "proxy_use_secret", "true")
	cfg.Set("couch_httpd_auth", "secret", "mysecret")

	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "alice")
	req.Header.Set("X-Auth-CouchDB-Token", "badtoken")

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestProxyAuth_MissingHeader(t *testing.T) {
	cfg := newProxyConfig()
	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestProxyAuth_CustomHeaderNames(t *testing.T) {
	cfg := newProxyConfig()
	cfg.Set("couch_httpd_auth", "x_auth_username", "X-Remote-User")
	cfg.Set("couch_httpd_auth", "x_auth_roles", "X-Remote-Roles")

	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Remote-User", "charlie")
	req.Header.Set("X-Remote-Roles", "admin")

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "charlie", s.Name)
	assert.Equal(t, []string{"admin"}, s.Roles)
}

func TestProxyAuth_SecretRequiredButMissing(t *testing.T) {
	cfg := newProxyConfig()
	cfg.Set("couch_httpd_auth", "proxy_use_secret", "true")
	// secret is empty

	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "alice")
	req.Header.Set("X-Auth-CouchDB-Token", "something")

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestProxyAuth_SecretRequiredButTokenMissing(t *testing.T) {
	cfg := newProxyConfig()
	cfg.Set("couch_httpd_auth", "proxy_use_secret", "true")
	cfg.Set("couch_httpd_auth", "secret", "mysecret")

	h := &ProxyAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Auth-CouchDB-UserName", "alice")
	// no token header

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}
