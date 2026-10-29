package goydb

import (
	"context"
	"testing"

	"github.com/goydb/goydb/pkg/client"
	"github.com/goydb/goydb/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestNewLocalClient_Basic(t *testing.T) {
	cfg := newTestConfig(t)
	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)
	defer gdb.Close() //nolint:errcheck

	c, closeClient, err := gdb.NewLocalClient("alice")
	require.NoError(t, err)
	defer closeClient() //nolint:errcheck

	ctx := context.Background()
	require.NoError(t, c.Up(ctx))

	db := c.DB("testdb")
	require.NoError(t, db.Create(ctx))
	rev, err := db.Put(ctx, &model.Document{ID: "doc1", Data: map[string]interface{}{"hello": "world"}})
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	doc, err := db.Get(ctx, "doc1")
	require.NoError(t, err)
	require.Equal(t, "world", doc.Data["hello"])
}

func TestNewLocalClient_MultipleIndependentClientsShareStorage(t *testing.T) {
	cfg := newTestConfig(t)
	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)
	defer gdb.Close() //nolint:errcheck

	ctx := context.Background()

	c1, close1, err := gdb.NewLocalClient("alice")
	require.NoError(t, err)
	defer close1() //nolint:errcheck

	c2, close2, err := gdb.NewLocalClient("bob")
	require.NoError(t, err)
	defer close2() //nolint:errcheck

	c3, close3, err := gdb.NewLocalClient("alice") // same name reused -- still an independent instance
	require.NoError(t, err)
	defer close3() //nolint:errcheck

	require.NoError(t, c1.DB("shared").Create(ctx))
	_, err = c1.DB("shared").Put(ctx, &model.Document{ID: "from-c1", Data: map[string]interface{}{"n": 1}})
	require.NoError(t, err)
	_, err = c2.DB("shared").Put(ctx, &model.Document{ID: "from-c2", Data: map[string]interface{}{"n": 2}})
	require.NoError(t, err)

	// All three independent clients see both documents -- same underlying storage.
	for _, c := range []*client.Client{c1, c2, c3} {
		doc1, err := c.DB("shared").Get(ctx, "from-c1")
		require.NoError(t, err)
		require.EqualValues(t, 1, doc1.Data["n"])

		doc2, err := c.DB("shared").Get(ctx, "from-c2")
		require.NoError(t, err)
		require.EqualValues(t, 2, doc2.Data["n"])
	}
}

func TestNewLocalClient_CloseDoesNotAffectGdbOrOtherClients(t *testing.T) {
	cfg := newTestConfig(t)
	gdb, err := cfg.BuildDatabase()
	require.NoError(t, err)
	defer gdb.Close() //nolint:errcheck

	ctx := context.Background()

	c1, close1, err := gdb.NewLocalClient("alice")
	require.NoError(t, err)

	require.NoError(t, c1.DB("testdb").Create(ctx))
	require.NoError(t, close1())

	// gdb itself, and a fresh client against it, still work after c1 closed.
	c2, close2, err := gdb.NewLocalClient("bob")
	require.NoError(t, err)
	defer close2() //nolint:errcheck

	exists, err := c2.DB("testdb").Exists(ctx)
	require.NoError(t, err)
	require.True(t, exists, "database created via the now-closed client must still be visible")
}
