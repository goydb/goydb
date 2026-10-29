//go:build !nototp

package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// totpCode generates a current TOTP code for the given base32 key.
func totpCode(base32Key string) string {
	secret, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(base32Key)
	counter := time.Now().Unix() / 30
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	code := int64(binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff)
	code = code % int64(math.Pow10(6))

	return fmt.Sprintf("%06d", code)
}

func TestSessionPost_AdminBypassTOTP(t *testing.T) {
	_, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	// Admin users skip TOTP — they have no user document with TOTP config.
	_, err := c.CreateSession(t.Context(), "admin", "secret")
	assert.NoError(t, err)
}

func TestSessionPost_UserWithoutTOTP(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	// Create _users database and add a user without TOTP.
	db, err := s.CreateDatabase(ctx, "_users")
	require.NoError(t, err)

	user := model.User{
		Name:     "testuser",
		Roles:    []string{},
		Type:     "user",
		Password: "testpass",
	}
	require.NoError(t, user.GeneratePBKDF2())

	data := map[string]interface{}{
		"name":            user.Name,
		"roles":           user.Roles,
		"type":            user.Type,
		"password_scheme": "pbkdf2",
		"iterations":      user.Iterations,
		"derived_key":     user.DerivedKey,
		"salt":            user.Salt,
	}
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "org.couchdb.user:testuser",
		Data: data,
	})
	require.NoError(t, err)

	_, err = c.CreateSession(ctx, "testuser", "testpass")
	assert.NoError(t, err)
}

func TestSessionPost_UserWithTOTP_ValidToken(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	db, err := s.CreateDatabase(ctx, "_users")
	require.NoError(t, err)

	totpKey := "JBSWY3DPEHPK3PXP"

	user := model.User{
		Name:     "totpuser",
		Roles:    []string{},
		Type:     "user",
		Password: "totppass",
	}
	require.NoError(t, user.GeneratePBKDF2())

	data := map[string]interface{}{
		"name":            user.Name,
		"roles":           user.Roles,
		"type":            user.Type,
		"password_scheme": "pbkdf2",
		"iterations":      user.Iterations,
		"derived_key":     user.DerivedKey,
		"salt":            user.Salt,
		"totp": map[string]interface{}{
			"key": totpKey,
		},
	}
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "org.couchdb.user:totpuser",
		Data: data,
	})
	require.NoError(t, err)

	code := totpCode(totpKey)

	// CreateSession doesn't support a "token" field, so POST /_session
	// directly for TOTP login.
	body, _ := json.Marshal(map[string]string{
		"name":     "totpuser",
		"password": "totppass",
		"token":    code,
	})
	resp, err := c.Raw(ctx, http.MethodPost, "/_session", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionPost_UserWithTOTP_MissingToken(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	db, err := s.CreateDatabase(ctx, "_users")
	require.NoError(t, err)

	totpKey := "JBSWY3DPEHPK3PXP"

	user := model.User{
		Name:     "totpuser2",
		Roles:    []string{},
		Type:     "user",
		Password: "totppass",
	}
	require.NoError(t, user.GeneratePBKDF2())

	data := map[string]interface{}{
		"name":            user.Name,
		"roles":           user.Roles,
		"type":            user.Type,
		"password_scheme": "pbkdf2",
		"iterations":      user.Iterations,
		"derived_key":     user.DerivedKey,
		"salt":            user.Salt,
		"totp": map[string]interface{}{
			"key": totpKey,
		},
	}
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "org.couchdb.user:totpuser2",
		Data: data,
	})
	require.NoError(t, err)

	// No token provided.
	_, err = c.CreateSession(ctx, "totpuser2", "totppass")
	assert.Equal(t, http.StatusUnauthorized, statusCode(t, err, http.StatusOK))
}

func TestSessionPost_UserWithTOTP_InvalidToken(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()

	db, err := s.CreateDatabase(ctx, "_users")
	require.NoError(t, err)

	totpKey := "JBSWY3DPEHPK3PXP"

	user := model.User{
		Name:     "totpuser3",
		Roles:    []string{},
		Type:     "user",
		Password: "totppass",
	}
	require.NoError(t, user.GeneratePBKDF2())

	data := map[string]interface{}{
		"name":            user.Name,
		"roles":           user.Roles,
		"type":            user.Type,
		"password_scheme": "pbkdf2",
		"iterations":      user.Iterations,
		"derived_key":     user.DerivedKey,
		"salt":            user.Salt,
		"totp": map[string]interface{}{
			"key": totpKey,
		},
	}
	_, err = db.PutDocument(ctx, &model.Document{
		ID:   "org.couchdb.user:totpuser3",
		Data: data,
	})
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{
		"name":     "totpuser3",
		"password": "totppass",
		"token":    "000000",
	})
	resp, err := c.Raw(ctx, http.MethodPost, "/_session", bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
