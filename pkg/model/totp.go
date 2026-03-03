package model

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// TOTPConfig holds TOTP configuration for a user.
type TOTPConfig struct {
	Key string `mapstructure:"key" json:"key"`
}

// VerifyTOTP verifies a 6-digit TOTP token against the stored key.
// It checks the current time step and ±1 step window (RFC 6238).
func (c *TOTPConfig) VerifyTOTP(token string) (bool, error) {
	if c == nil || c.Key == "" {
		return false, fmt.Errorf("TOTP not configured")
	}

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(
		strings.ToUpper(strings.TrimSpace(c.Key)),
	)
	if err != nil {
		return false, fmt.Errorf("invalid TOTP key: %w", err)
	}

	now := time.Now().Unix()
	timeStep := int64(30)
	currentCounter := now / timeStep

	// Check ±1 window.
	for _, offset := range []int64{0, -1, 1} {
		code := generateTOTP(key, currentCounter+offset)
		if code == token {
			return true, nil
		}
	}

	return false, nil
}

// generateTOTP generates a 6-digit TOTP code for the given counter.
func generateTOTP(key []byte, counter int64) string {
	// RFC 4226: HOTP(K,C) = Truncate(HMAC-SHA1(K,C))
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	// Dynamic truncation.
	offset := h[len(h)-1] & 0x0f
	code := int64(binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff)
	code = code % int64(math.Pow10(6))

	return fmt.Sprintf("%06d", code)
}
