package handler

import (
	"net/http"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowFunction_NotImplemented(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	_, err = c.DB("testdb").Show(t.Context(), "mydesign", "myfunc", "")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotImplemented, statusCode(t, err, http.StatusOK))
}

func TestListFunction_NotImplemented(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	_, err = c.DB("testdb").List(t.Context(), "mydesign", "myfunc", "myview")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotImplemented, statusCode(t, err, http.StatusOK))
}

func TestUpdateFunction_MissingDesignDoc(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	_, err = c.DB("testdb").Update(t.Context(), "mydesign", "myfunc", "", nil)
	require.Error(t, err)
	var se *client.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusNotFound, se.StatusCode)
}

func TestRewrite_NotImplemented(t *testing.T) {
	s, c, cleanup := setupRevsDiffTest(t)
	defer cleanup()

	_, err := s.CreateDatabase(t.Context(), "testdb")
	require.NoError(t, err)

	_, err = c.DB("testdb").Rewrite(t.Context(), "mydesign", "some/path")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotImplemented, statusCode(t, err, http.StatusOK))
}
