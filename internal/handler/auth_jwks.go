//go:build !nojwt

package handler

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// JWK represents a single JSON Web Key.
type JWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	// RSA fields
	N string `json:"n"`
	E string `json:"e"`
	// EC fields
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// jwksResponse is the standard JWKS endpoint response.
type jwksResponse struct {
	Keys []JWK `json:"keys"`
}

// JWKSCache fetches and caches keys from a remote JWKS URL.
type JWKSCache struct {
	url       string
	ttl       time.Duration
	mu        sync.RWMutex
	keys      []JWK
	fetchedAt time.Time
	client    *http.Client
}

// NewJWKSCache creates a new cache for the given URL.
func NewJWKSCache(url string, ttl time.Duration) *JWKSCache {
	return &JWKSCache{
		url:    url,
		ttl:    ttl,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetKey returns the parsed crypto key matching the kid. It refreshes the
// cache when stale or when the kid is not found (key rotation handling).
func (c *JWKSCache) GetKey(kid string) (interface{}, error) {
	c.mu.RLock()
	key, err := c.findKey(kid)
	stale := time.Since(c.fetchedAt) > c.ttl
	c.mu.RUnlock()

	if err == nil && !stale {
		return key, nil
	}

	// Refresh and retry.
	if err := c.FetchKeys(); err != nil {
		return nil, fmt.Errorf("jwks fetch failed: %w", err)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.findKey(kid)
}

// FetchKeys fetches the JWKS endpoint and updates the cache.
func (c *JWKSCache) FetchKeys() error {
	resp, err := c.client.Get(c.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks endpoint returned %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return err
	}

	c.mu.Lock()
	c.keys = jwks.Keys
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return nil
}

func (c *JWKSCache) findKey(kid string) (interface{}, error) {
	for _, k := range c.keys {
		if k.Kid == kid {
			return parseJWK(k)
		}
	}
	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

// parseJWK converts a JWK into a Go crypto public key.
func parseJWK(k JWK) (interface{}, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAKey(k)
	case "EC":
		return parseECKey(k)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", k.Kty)
	}
}

func parseRSAKey(k JWK) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nb)
	e := new(big.Int).SetBytes(eb)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}

func parseECKey(k JWK) (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported curve: %s", k.Crv)
	}

	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}
