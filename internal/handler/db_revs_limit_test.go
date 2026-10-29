package handler

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRevsLimitGet_Default(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	limit, err := c.DB("testdb").GetRevsLimit(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1000, limit)
}

func TestRevsLimitPut_Valid(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	err = c.DB("testdb").SetRevsLimit(t.Context(), 500)
	require.NoError(t, err)
}

func TestRevsLimitGet_AfterPut(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	// PUT 500
	err = c.DB("testdb").SetRevsLimit(t.Context(), 500)
	require.NoError(t, err)

	// GET should now return 500
	limit, err := c.DB("testdb").GetRevsLimit(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 500, limit)
}

func TestRevsLimitPut_InvalidBody(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	tests := []struct {
		name string
		body []byte
	}{
		{"zero", []byte("0")},
		{"negative", []byte("-5")},
		{"not a number", []byte(`"hello"`)},
		{"empty", []byte("")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.DB("testdb").Raw(t.Context(), http.MethodPut, "/_revs_limit", bytes.NewReader(tc.body), map[string]string{"Content-Type": "application/json"})
			require.NoError(t, err)
			defer resp.Body.Close() //nolint:errcheck
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}
