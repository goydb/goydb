//go:build !nototp

package model

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestTOTP generates a TOTP code for the given secret and time.
func generateTestTOTP(secret []byte, t time.Time) string {
	counter := t.Unix() / 30
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

func TestTOTP_VerifyCurrentCode(t *testing.T) {
	secret := []byte("12345678901234567890")
	key := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)

	cfg := &TOTPConfig{Key: key}

	code := generateTestTOTP(secret, time.Now())
	ok, err := cfg.VerifyTOTP(code)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTOTP_VerifyWindowMinus1(t *testing.T) {
	secret := []byte("12345678901234567890")
	key := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)

	cfg := &TOTPConfig{Key: key}

	// Generate code for previous time step.
	code := generateTestTOTP(secret, time.Now().Add(-30*time.Second))
	ok, err := cfg.VerifyTOTP(code)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestTOTP_WrongCode(t *testing.T) {
	secret := []byte("12345678901234567890")
	key := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)

	cfg := &TOTPConfig{Key: key}

	ok, err := cfg.VerifyTOTP("000000")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestTOTP_NotConfigured(t *testing.T) {
	cfg := &TOTPConfig{}
	_, err := cfg.VerifyTOTP("123456")
	assert.Error(t, err)
}

func TestTOTP_NilConfig(t *testing.T) {
	var cfg *TOTPConfig
	_, err := cfg.VerifyTOTP("123456")
	assert.Error(t, err)
}

func TestUser_HasTOTP(t *testing.T) {
	u := &User{}
	assert.False(t, u.HasTOTP())

	u.TOTP = &TOTPConfig{}
	assert.False(t, u.HasTOTP())

	u.TOTP = &TOTPConfig{Key: "JBSWY3DPEHPK3PXP"}
	assert.True(t, u.HasTOTP())
}
