package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBUpdates_NoDatabases(t *testing.T) {
	_, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/_db_updates", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []interface{} `json:"results"`
		LastSeq string        `json:"last_seq"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.Empty(t, result.Results)
	assert.Equal(t, "0", result.LastSeq)
}

func TestDBUpdates_WithDatabases(t *testing.T) {
	s, router, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	ctx := t.Context()
	_, err := s.CreateDatabase(ctx, "db1")
	require.NoError(t, err)
	_, err = s.CreateDatabase(ctx, "db2")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/_db_updates", nil)
	req.SetBasicAuth("admin", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Results []struct {
			DBName string `json:"db_name"`
			Type   string `json:"type"`
			Seq    string `json:"seq"`
		} `json:"results"`
		LastSeq string `json:"last_seq"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
	assert.GreaterOrEqual(t, len(result.Results), 2)

	// Check that all events are "updated" type
	for _, ev := range result.Results {
		assert.Equal(t, "updated", ev.Type)
		assert.NotEmpty(t, ev.DBName)
		assert.NotEmpty(t, ev.Seq)
	}
	assert.NotEqual(t, "0", result.LastSeq)
}
