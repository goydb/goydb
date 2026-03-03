package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/goydb/goydb/pkg/model"
)

func init() {
	RegisterAuthHandler("jwt_authentication_handler", func(b Base) AuthHandler {
		return &JWTAuthHandler{Config: b.Config}
	})
}

// JWTAuthHandler validates externally-issued JWTs from the Authorization: Bearer header.
type JWTAuthHandler struct {
	Config *ConfigStore

	mu        sync.Mutex
	jwksCache *JWKSCache
}

func (h *JWTAuthHandler) Name() string { return "jwt" }

func (h *JWTAuthHandler) Authenticate(r *http.Request) (*model.Session, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, false
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")

	token, err := jwt.Parse(tokenString, h.keyFunc, jwt.WithValidMethods([]string{
		"HS256", "HS384", "HS512",
		"RS256", "RS384", "RS512",
		"ES256", "ES384", "ES512",
	}))
	if err != nil || !token.Valid {
		return nil, false
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false
	}

	// Check required claims.
	if !h.checkRequiredClaims(claims) {
		return nil, false
	}

	// Extract username from sub claim.
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, false
	}

	// Extract roles.
	roles := h.extractRoles(claims)
	if roles == nil {
		roles = []string{}
	}

	return &model.Session{
		Name:  sub,
		Roles: roles,
	}, true
}

// keyFunc resolves the signing key for token validation.
func (h *JWTAuthHandler) keyFunc(token *jwt.Token) (interface{}, error) {
	alg := token.Method.Alg()
	kid, _ := token.Header["kid"].(string)

	// 1. Try static keys from jwt_keys config section.
	if key, err := h.resolveStaticKey(alg, kid); err == nil {
		return key, nil
	}

	// 2. Try JWKS URL.
	if kid != "" {
		jwksURL, _ := h.Config.Get("jwt_auth", "jwks_url")
		if jwksURL != "" {
			cache := h.getJWKSCache(jwksURL)
			if key, err := cache.GetKey(kid); err == nil {
				return key, nil
			}
		}
	}

	return nil, fmt.Errorf("no key found for alg=%s kid=%s", alg, kid)
}

// resolveStaticKey looks up a key in the jwt_keys config section.
// Keys are stored as "{alg}:{kid}" → base64-encoded key material, or just
// "{alg}" → key material when there is no kid.
func (h *JWTAuthHandler) resolveStaticKey(alg, kid string) (interface{}, error) {
	section, ok := h.Config.Section("jwt_keys")
	if !ok {
		return nil, fmt.Errorf("no jwt_keys section")
	}

	// Try alg:kid first, then alg alone.
	var raw string
	if kid != "" {
		if v, ok := section[alg+":"+kid]; ok {
			raw = v
		}
	}
	if raw == "" {
		if v, ok := section[alg]; ok {
			raw = v
		}
	}
	if raw == "" {
		return nil, fmt.Errorf("no key for alg=%s kid=%s", alg, kid)
	}

	return decodeKey(alg, raw)
}

// decodeKey interprets the raw key material based on the algorithm.
func decodeKey(alg, raw string) (interface{}, error) {
	switch {
	case strings.HasPrefix(alg, "HS"):
		// HMAC: key material is a plain string secret (CouchDB-compatible).
		return []byte(raw), nil

	case strings.HasPrefix(alg, "RS"):
		return jwt.ParseRSAPublicKeyFromPEM([]byte(raw))

	case strings.HasPrefix(alg, "ES"):
		return jwt.ParseECPublicKeyFromPEM([]byte(raw))

	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", alg)
	}
}

func (h *JWTAuthHandler) checkRequiredClaims(claims jwt.MapClaims) bool {
	raw, _ := h.Config.Get("jwt_auth", "required_claims")
	if raw == "" {
		return true
	}
	for _, part := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, expected := kv[0], kv[1]
		actual, ok := claims[key]
		if !ok {
			return false
		}
		if fmt.Sprint(actual) != expected {
			return false
		}
	}
	return true
}

func (h *JWTAuthHandler) extractRoles(claims jwt.MapClaims) []string {
	path, _ := h.Config.Get("jwt_auth", "roles_claim_path")
	if path == "" {
		path = "_couchdb.roles"
	}

	var val interface{} = map[string]interface{}(claims)
	for _, part := range strings.Split(path, ".") {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil
		}
		val = m[part]
	}

	switch v := val.(type) {
	case []interface{}:
		roles := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				roles = append(roles, s)
			}
		}
		return roles
	case string:
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	default:
		return nil
	}
}

func (h *JWTAuthHandler) getJWKSCache(url string) *JWKSCache {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.jwksCache != nil && h.jwksCache.url == url {
		return h.jwksCache
	}

	ttl := 3600
	if raw, ok := h.Config.Get("jwt_auth", "jwks_cache_ttl"); ok {
		if v, err := strconv.Atoi(raw); err == nil {
			ttl = v
		}
	}

	h.jwksCache = NewJWKSCache(url, time.Duration(ttl)*time.Second)
	return h.jwksCache
}

