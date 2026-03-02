package handler

import (
	"testing"

	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashUserPassword_NonUsersDB_Noop(t *testing.T) {
	doc := &model.Document{
		ID: "org.couchdb.user:alice",
		Data: map[string]interface{}{
			"name":     "alice",
			"password": "secret",
			"type":     "user",
			"roles":    []string{},
		},
	}

	err := hashUserPassword("mydb", doc)
	require.NoError(t, err)

	// password should remain untouched
	assert.Equal(t, "secret", doc.Data["password"])
	assert.Nil(t, doc.Data["derived_key"])
}

func TestHashUserPassword_UsersDB_WithPassword(t *testing.T) {
	doc := &model.Document{
		ID: "org.couchdb.user:alice",
		Data: map[string]interface{}{
			"name":     "alice",
			"password": "secret",
			"type":     "user",
			"roles":    []string{},
		},
	}

	err := hashUserPassword("_users", doc)
	require.NoError(t, err)

	// plaintext password must be removed
	_, hasPassword := doc.Data["password"]
	assert.False(t, hasPassword, "password field should be removed")

	// derived fields must be set
	assert.NotEmpty(t, doc.Data["derived_key"])
	assert.NotEmpty(t, doc.Data["salt"])
	assert.Equal(t, "pbkdf2", doc.Data["password_scheme"])
	iterations, ok := doc.Data["iterations"].(int)
	require.True(t, ok, "iterations should be an int")
	assert.Greater(t, iterations, 0)

	// verify the derived key actually works
	var u model.User
	require.NoError(t, u.FromDocument(doc))
	ok, err = u.VerifyPassword("secret")
	require.NoError(t, err)
	assert.True(t, ok, "derived key should verify against original password")
}

func TestHashUserPassword_UsersDB_NoPassword_Noop(t *testing.T) {
	doc := &model.Document{
		ID: "org.couchdb.user:alice",
		Data: map[string]interface{}{
			"name":        "alice",
			"type":        "user",
			"roles":       []string{},
			"derived_key": "existing",
			"salt":        "existing",
		},
	}

	err := hashUserPassword("_users", doc)
	require.NoError(t, err)

	// existing derived_key should not be changed
	assert.Equal(t, "existing", doc.Data["derived_key"])
	assert.Equal(t, "existing", doc.Data["salt"])
}

func TestHashUserPassword_UsersDB_EmptyPassword_Noop(t *testing.T) {
	doc := &model.Document{
		ID: "org.couchdb.user:alice",
		Data: map[string]interface{}{
			"name":     "alice",
			"password": "",
			"type":     "user",
			"roles":    []string{},
		},
	}

	err := hashUserPassword("_users", doc)
	require.NoError(t, err)

	// empty password is treated as no password
	assert.Equal(t, "", doc.Data["password"])
	assert.Nil(t, doc.Data["derived_key"])
}

func TestHashUserPassword_DeletedDoc_Noop(t *testing.T) {
	doc := &model.Document{
		ID:      "org.couchdb.user:alice",
		Deleted: true,
		Data: map[string]interface{}{
			"password": "secret",
		},
	}

	err := hashUserPassword("_users", doc)
	require.NoError(t, err)

	assert.Equal(t, "secret", doc.Data["password"])
}
