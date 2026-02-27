package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasRevision_CurrentRev(t *testing.T) {
	doc := Document{Rev: "2-abc"}
	assert.True(t, doc.HasRevision("2-abc"))
}

func TestHasRevision_AncestorRev(t *testing.T) {
	doc := Document{Rev: "3-abc", RevHistory: []string{"3-abc", "2-def", "1-ghi"}}
	assert.True(t, doc.HasRevision("2-def"))
	assert.True(t, doc.HasRevision("1-ghi"))
}

func TestHasRevision_Missing(t *testing.T) {
	doc := Document{Rev: "2-abc", RevHistory: []string{"2-abc", "1-xyz"}}
	assert.False(t, doc.HasRevision("1-unknown"))
}

func TestHasRevision_EmptyHistory(t *testing.T) {
	doc := Document{Rev: "2-abc"}
	assert.True(t, doc.HasRevision("2-abc"))
	assert.False(t, doc.HasRevision("1-old"))
}

func TestRevisions_WithHistory(t *testing.T) {
	doc := Document{
		Rev:        "3-abc",
		RevHistory: []string{"3-abc", "2-def", "1-ghi"},
	}
	revs := doc.Revisions()
	assert.Equal(t, int64(3), revs.Start)
	assert.Equal(t, []string{"abc", "def", "ghi"}, revs.IDs)
}

func TestRevisions_FallbackNoHistory(t *testing.T) {
	doc := Document{Rev: "2-xyz"}
	revs := doc.Revisions()
	assert.Equal(t, int64(2), revs.Start)
	assert.Equal(t, []string{"xyz"}, revs.IDs)
}

func TestWinnerRev_HigherGenWins(t *testing.T) {
	assert.Equal(t, "2-a", WinnerRev([]string{"1-z", "2-a"}))
}

func TestWinnerRev_TieBreakerHash(t *testing.T) {
	assert.Equal(t, "1-abc", WinnerRev([]string{"1-aab", "1-abc"}))
}

func TestWinnerRev_Single(t *testing.T) {
	assert.Equal(t, "3-xyz", WinnerRev([]string{"3-xyz"}))
}

func TestNextLocalRevision_EmptyRev(t *testing.T) {
	doc := Document{}
	assert.Equal(t, "0-1", doc.NextLocalRevision())
}

func TestNextLocalRevision_Increment(t *testing.T) {
	doc := Document{Rev: "0-1"}
	assert.Equal(t, "0-2", doc.NextLocalRevision())

	doc.Rev = "0-5"
	assert.Equal(t, "0-6", doc.NextLocalRevision())

	doc.Rev = "0-99"
	assert.Equal(t, "0-100", doc.NextLocalRevision())
}

func TestNextLocalRevision_MigrationFromContentHash(t *testing.T) {
	// A _local doc that was stored with the old content-hash scheme
	// should migrate to "0-1" on next write.
	doc := Document{Rev: "1-abc123"}
	assert.Equal(t, "0-1", doc.NextLocalRevision())
}
