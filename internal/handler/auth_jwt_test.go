//go:build !nojwt

package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newJWTConfig() *ConfigStore {
	return NewConfigStore("", nil)
}

func signHS256(claims jwt.MapClaims, secret string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(secret))
	return s
}

func signRS256(claims jwt.MapClaims, key *rsa.PrivateKey, kid string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	s, _ := token.SignedString(key)
	return s
}

func signES256(claims jwt.MapClaims, key *ecdsa.PrivateKey, kid string) string {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	if kid != "" {
		token.Header["kid"] = kid
	}
	s, _ := token.SignedString(key)
	return s
}

func rsaPublicKeyPEM(pub *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func ecPublicKeyPEM(pub *ecdsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func TestJWT_HS256_Valid(t *testing.T) {
	cfg := newJWTConfig()
	secret := "supersecretkey"
	cfg.Set("jwt_keys", "HS256", secret)

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub":      "alice",
		"_couchdb": map[string]interface{}{"roles": []interface{}{"reader"}},
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "alice", s.Name)
	assert.Equal(t, []string{"reader"}, s.Roles)
}

func TestJWT_HS256_InvalidSignature(t *testing.T) {
	cfg := newJWTConfig()
	cfg.Set("jwt_keys", "HS256", "correctsecret")

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, "wrongsecret")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_HS256_Expired(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256", secret)

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(-time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_RS256_Valid(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	cfg := newJWTConfig()
	cfg.Set("jwt_keys", "RS256", rsaPublicKeyPEM(&key.PublicKey))

	h := &JWTAuthHandler{Config: cfg}

	tok := signRS256(jwt.MapClaims{
		"sub":      "bob",
		"_couchdb": map[string]interface{}{"roles": []interface{}{"admin"}},
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}, key, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "bob", s.Name)
	assert.Equal(t, []string{"admin"}, s.Roles)
}

func TestJWT_ES256_Valid(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	cfg := newJWTConfig()
	cfg.Set("jwt_keys", "ES256", ecPublicKeyPEM(&key.PublicKey))

	h := &JWTAuthHandler{Config: cfg}

	tok := signES256(jwt.MapClaims{
		"sub":      "carol",
		"_couchdb": map[string]interface{}{"roles": []interface{}{"writer"}},
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}, key, "")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "carol", s.Name)
	assert.Equal(t, []string{"writer"}, s.Roles)
}

func TestJWT_MissingRequiredClaims(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256", secret)
	cfg.Set("jwt_auth", "required_claims", "iss=myapp")

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_RequiredClaimsPresent(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256", secret)
	cfg.Set("jwt_auth", "required_claims", "iss=myapp")

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"iss": "myapp",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "alice", s.Name)
}

func TestJWT_CustomRolesPath(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256", secret)
	cfg.Set("jwt_auth", "roles_claim_path", "realm_access.roles")

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub":          "alice",
		"realm_access": map[string]interface{}{"roles": []interface{}{"admin", "user"}},
		"exp":          float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, []string{"admin", "user"}, s.Roles)
}

func TestJWT_NoSubClaim(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256", secret)

	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_NoKeysConfigured(t *testing.T) {
	cfg := newJWTConfig()
	h := &JWTAuthHandler{Config: cfg}

	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, "somesecret")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_KidMismatch(t *testing.T) {
	cfg := newJWTConfig()
	secret := "mysecret"
	cfg.Set("jwt_keys", "HS256:key1", secret)

	h := &JWTAuthHandler{Config: cfg}

	// Token signed without kid — static lookup will try "HS256" (no kid) and fail.
	tok := signHS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_NonBearerPrefix(t *testing.T) {
	cfg := newJWTConfig()
	h := &JWTAuthHandler{Config: cfg}

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic abc123")

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

// --- JWKS tests ---

func TestJWT_JWKS_RS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	kid := "test-kid-1"

	// Serve a JWKS endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "RSA",
					"kid": kid,
					"alg": "RS256",
					"use": "sig",
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				},
			},
		}
		json.NewEncoder(w).Encode(jwks) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := newJWTConfig()
	cfg.Set("jwt_auth", "jwks_url", srv.URL)

	h := &JWTAuthHandler{Config: cfg}

	tok := signRS256(jwt.MapClaims{
		"sub":      "dave",
		"_couchdb": map[string]interface{}{"roles": []interface{}{"reader"}},
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}, key, kid)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "dave", s.Name)
	assert.Equal(t, []string{"reader"}, s.Roles)
}

func TestJWT_JWKS_CacheTTL(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	kid := "ttl-kid"
	fetchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "RSA",
					"kid": kid,
					"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
					"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
				},
			},
		}
		json.NewEncoder(w).Encode(jwks) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := newJWTConfig()
	cfg.Set("jwt_auth", "jwks_url", srv.URL)
	cfg.Set("jwt_auth", "jwks_cache_ttl", "1") // 1 second TTL

	h := &JWTAuthHandler{Config: cfg}

	tok := signRS256(jwt.MapClaims{
		"sub": "eve",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, key, kid)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	// First call — fetches.
	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "eve", s.Name)
	firstFetch := fetchCount

	// Second call within TTL — uses cache.
	_, ok = h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, firstFetch, fetchCount, "should use cached keys")

	// Wait for TTL to expire.
	time.Sleep(1100 * time.Millisecond)

	_, ok = h.Authenticate(req)
	require.True(t, ok)
	assert.Greater(t, fetchCount, firstFetch, "should have re-fetched after TTL")
}

func TestJWT_JWKS_KeyRotation(t *testing.T) {
	key1, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key2, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	serveKid2 := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys := []map[string]interface{}{
			{
				"kty": "RSA",
				"kid": "kid1",
				"n":   base64.RawURLEncoding.EncodeToString(key1.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key1.E)).Bytes()),
			},
		}
		if serveKid2 {
			keys = append(keys, map[string]interface{}{
				"kty": "RSA",
				"kid": "kid2",
				"n":   base64.RawURLEncoding.EncodeToString(key2.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key2.E)).Bytes()),
			})
		}
		jwks := map[string]interface{}{"keys": keys}
		json.NewEncoder(w).Encode(jwks) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := newJWTConfig()
	cfg.Set("jwt_auth", "jwks_url", srv.URL)

	h := &JWTAuthHandler{Config: cfg}

	// Token signed with key1/kid1 should work.
	tok1 := signRS256(jwt.MapClaims{
		"sub": "frank",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, key1, "kid1")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok1)
	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "frank", s.Name)

	// Token signed with key2/kid2 should fail initially (kid2 not in JWKS).
	tok2 := signRS256(jwt.MapClaims{
		"sub": "grace",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, key2, "kid2")

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	_, ok = h.Authenticate(req2)
	assert.False(t, ok)

	// Simulate key rotation: add kid2 to JWKS, clear cache.
	serveKid2 = true
	h.jwksCache = nil

	s, ok = h.Authenticate(req2)
	require.True(t, ok)
	assert.Equal(t, "grace", s.Name)
}

func TestJWT_JWKS_Unreachable(t *testing.T) {
	cfg := newJWTConfig()
	cfg.Set("jwt_auth", "jwks_url", "http://127.0.0.1:1") // unreachable

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	h := &JWTAuthHandler{Config: cfg}

	tok := signRS256(jwt.MapClaims{
		"sub": "alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}, key, "some-kid")

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	_, ok := h.Authenticate(req)
	assert.False(t, ok)
}

func TestJWT_JWKS_ECKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	kid := "ec-kid-1"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks := map[string]interface{}{
			"keys": []map[string]interface{}{
				{
					"kty": "EC",
					"kid": kid,
					"crv": "P-256",
					"x":   base64.RawURLEncoding.EncodeToString(key.PublicKey.X.Bytes()),
					"y":   base64.RawURLEncoding.EncodeToString(key.PublicKey.Y.Bytes()),
				},
			},
		}
		json.NewEncoder(w).Encode(jwks) //nolint:errcheck
	}))
	defer srv.Close()

	cfg := newJWTConfig()
	cfg.Set("jwt_auth", "jwks_url", srv.URL)

	h := &JWTAuthHandler{Config: cfg}

	tok := signES256(jwt.MapClaims{
		"sub":      "heidi",
		"_couchdb": map[string]interface{}{"roles": []interface{}{"editor"}},
		"exp":      float64(time.Now().Add(time.Hour).Unix()),
	}, key, kid)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	s, ok := h.Authenticate(req)
	require.True(t, ok)
	assert.Equal(t, "heidi", s.Name)
	assert.Equal(t, []string{"editor"}, s.Roles)
}
